import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, ChevronRight, Link2, Table2 } from 'lucide-react'
import { Button, DataTable, Dialog, FeedbackState } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { getBojunReferenceTable } from '../../bojunApi'
import type { BojunColumn, BojunTable } from '../../types'
import type { BojunMetadata } from '../ReportQueryDesigner/BojunMetadata'
import { ReportDefaultFields } from '../ReportQueryDesigner/ReportDefaultFields'
import { useBojunMetadata } from '../ReportQueryDesigner/useBojunMetadata'
import type { ReportReferenceStep } from '../ReportQueryDesigner/referenceJoin'
import { BojunReferenceField } from './BojunReferenceField'
import reportDesign from '../../reportDesign.module.css'
import styles from './BojunReferenceDialog.module.css'

export type BojunReferenceSelection = { aliasId?: string; tableId: string; tableName: string; column: BojunColumn; trigger: HTMLElement }
type ReferenceStep = Omit<BojunReferenceSelection, 'trigger'> & { target?: BojunMetadata[string] }

function useReferenceTable(client: ReportCenterClient, datasourceId: number, step: ReferenceStep, reload: number) {
  const identity = `${datasourceId}:${step.tableId}:${step.column.id}`
  const [state, setState] = useState<{ identity: string; table: BojunTable | null; loading: boolean; error: string }>({ identity, table: null, loading: true, error: '' })
  useEffect(() => {
    const controller = new AbortController()
    setState({ identity, table: null, loading: true, error: '' })
    void getBojunReferenceTable(client, datasourceId, step.tableId, String(step.column.id), controller.signal).then((response) => {
      if (controller.signal.aborted) return
      setState(response.ok ? { identity, table: response.data, loading: false, error: '' } : { identity, table: null, loading: false, error: response.error })
    })
    return () => controller.abort()
  }, [client, datasourceId, identity, step.tableId, step.column.id, reload])
  return state.identity === identity ? state : { table: null, loading: true, error: '' }
}

export function BojunReferenceDialog({ client, datasourceId, selection, onClose, onJoin }: { client: ReportCenterClient; datasourceId: number; selection: BojunReferenceSelection; onClose: () => void; onJoin?: (steps: ReportReferenceStep[]) => void }) {
  const [history, setHistory] = useState<ReferenceStep[]>([selection])
  const [reload, setReload] = useState(0)
  const [joinError, setJoinError] = useState('')
  const step = history[history.length - 1]
  const resolved = useReferenceTable(client, datasourceId, step, reload)
  const table = resolved.table
  const lookup = useBojunMetadata(client, datasourceId, table ? [String(table.id)] : [], reload)
  const item = table ? lookup.metadata[String(table.id)] : undefined
  const error = resolved.error || (table ? lookup.error : '')
  const name = table?.description || table?.logicalName
  const label = name || '正在定位关联表…'
  const path = [selection.tableName, ...history.slice(1).map((entry) => entry.tableName), label]
  function join() {
    if (!onJoin || !item || error) return
    try {
      const steps = history.map((entry, index): ReportReferenceStep => {
        const target = index === history.length - 1 ? item : entry.target
        if (!target) throw new Error('关联路径尚未读取完整，请返回上一张表后重试。')
        return { sourceTableId: entry.tableId, sourceColumnId: String(entry.column.id), target }
      })
      onJoin(steps)
    } catch (error) {
      setJoinError(error instanceof Error ? error.message : '加入关联表失败，请重试。')
    }
  }
  return <Dialog open className={`${reportDesign.surface} ${styles.dialog}`} title={<span className={styles.title}><Table2 aria-hidden="true" />关联表详情{name ? ` · ${name}` : ''}<small>{onJoin ? '字典关联' : '只读'}</small></span>} description={`来自 ${step.tableName} · ${step.column.dbName}，按伯俊字典关联查看表结构。`} closeLabel="关闭关联表详情" returnFocus={selection.trigger} onClose={onClose} footer={<><span className={styles.footerNote}>{onJoin ? '按访问路径关联，默认显示字段自动选中' : '查看详情保留当前报表与字段选择'}</span>{history.length > 1 ? <Button onClick={() => { setHistory((current) => current.slice(0, -1)); setJoinError('') }}><ArrowLeft aria-hidden="true" />返回上一张表</Button> : null}<Button onClick={onClose}>返回配置</Button>{onJoin ? <Button variant="primary" disabled={resolved.loading || !item || Boolean(error)} onClick={join}><Link2 aria-hidden="true" />加入并自动关联</Button> : null}</>}>
    <nav className={styles.path} aria-label="关联表访问路径">{path.map((part, index) => <span key={index}>{index ? <ChevronRight aria-hidden="true" /> : null}<span>{part}</span></span>)}</nav>
    {joinError ? <p className={styles.joinError} role="alert">{joinError}</p> : null}
    {error ? <FeedbackState kind="error" title="关联表详情暂不可用" description={error} action={<Button onClick={() => setReload((value) => value + 1)}>重新读取</Button>} /> : resolved.loading || !item ? <FeedbackState kind="loading" title="正在读取关联表与字段…" /> : <ReferenceFields key={`${step.tableId}:${step.column.id}`} item={item} matchingColumnId={step.column.refColumnId} onInspect={(column) => { setHistory((current) => [...current.slice(0, -1), { ...current[current.length - 1], target: item }, { tableId: String(item.table.id), tableName: item.table.description || item.table.logicalName, column }]); setJoinError('') }} />}
  </Dialog>
}

function ReferenceFields({ item, matchingColumnId, onInspect }: { item: BojunMetadata[string]; matchingColumnId: string; onInspect: (column: BojunColumn, trigger: HTMLButtonElement) => void }) {
  const headingRef = useRef<HTMLHeadingElement>(null)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState<number | null>(null)
  const { table, columns } = item
  const matching = columns.find((column) => column.bojunColumnId === matchingColumnId)
  const defaults = table.displayFields.length ? table.displayFields : table.primaryKeys
  const shown = columns.filter((column) => `${column.dbName} ${column.description} ${column.name}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  const pages = Math.max(1, Math.ceil(shown.length / 30))
  const matchIndex = matching ? shown.indexOf(matching) : -1
  const currentPage = Math.min(page ?? Math.max(0, Math.floor(matchIndex / 30)), pages - 1)
  useEffect(() => { headingRef.current?.focus() }, [])
  return <section className={styles.content} aria-label="关联表字段详情">
    <header className={styles.tableHeading}><div><h3 ref={headingRef} tabIndex={-1}>{table.description || table.logicalName}</h3><code>{table.logicalName}</code></div><span>{columns.length} 个字段 · 字典结构</span></header>
    <div className={styles.dictionary}><ReportDefaultFields table={table} columns={columns} /><div className={styles.matching}><span>关联匹配字段</span><strong>{matching?.description || matching?.dbName || '字典未指定'}</strong>{matching ? <code>{matching.dbName}</code> : null}</div></div>
    <div className={styles.toolbar}><label>查找字段<input name="reference-table-field-search" type="search" autoComplete="off" placeholder="字段编码或业务名称…" value={search} onChange={(event) => { setSearch(event.currentTarget.value); setPage(0) }} /></label><span>点击带箭头的字段可继续查看关联表</span></div>
    <DataTable density="compact" minWidth={520} containerClassName={styles.fieldScroll} scrollLabel="关联表只读字段列表"><thead><tr><th>字段编码</th><th>业务名称</th><th>类型</th><th>字典定义</th></tr></thead><tbody>{shown.slice(currentPage * 30, (currentPage + 1) * 30).map((column) => { const id = String(column.id); return <tr key={id} className={column === matching ? styles.matchedRow : undefined}><td><code>{column.dbName}</code></td><td><BojunReferenceField column={column} onInspect={onInspect} /></td><td><code>{column.oracleType || column.logicalType || '—'}</code></td><td><div className={styles.badges}>{column === matching ? <span className={styles.matchBadge}>关联匹配</span> : null}{defaults.includes(id) ? <span className={styles.defaultBadge}>默认显示</span> : null}{table.primaryKeys.includes(id) ? <span>主键</span> : null}{column.refTableId ? <span>关联</span> : null}{!column.isPhysical ? <span>虚拟字段</span> : null}</div></td></tr> })}</tbody></DataTable>
    {!shown.length ? <p className={styles.hint}>没有匹配的字段，请调整关键词。</p> : null}
    <div className={styles.pagination}><span>{shown.length} 个字段{table.resolvedTable ? ` · ${table.resolvedTable}` : ''}</span>{pages > 1 ? <div><span>{currentPage + 1} / {pages}</span><Button disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>上一页</Button><Button disabled={currentPage + 1 === pages} onClick={() => setPage(currentPage + 1)}>下一页</Button></div> : null}</div>
  </section>
}
