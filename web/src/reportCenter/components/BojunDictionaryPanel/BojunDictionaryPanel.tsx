import { useEffect, useState } from 'react'
import { Database, RefreshCw, Settings2 } from 'lucide-react'
import { Button, DataTable, FeedbackState, PaginationControls, StatusTag } from '../../../ui'
import { getBojunSync, startBojunSync } from '../../bojunApi'
import type { ReportCenterClient } from '../../api'
import type { BojunSyncRun, BojunTable, ReportDatasource } from '../../types'
import { BojunTableSelect } from '../ReportQueryDesigner/BojunMetadata'
import { useBojunMetadata } from '../ReportQueryDesigner/useBojunMetadata'
import { BojunTableConfigurationDialog } from './BojunTableConfigurationDialog'
import styles from './BojunDictionaryPanel.module.css'

const pendingStatuses = ['QUEUED', 'RUNNING', 'PENDING']
const statusLabels: Record<string, string> = { QUEUED: '等待同步', RUNNING: '正在同步', PENDING: '等待同步', SUCCEEDED: '同步完成', SUCCESS: '同步完成', FAILED: '同步失败' }

export function BojunDictionaryPanel({ client, datasources }: { client: ReportCenterClient; datasources: ReportDatasource[] }) {
  const [datasourceId, setDatasourceId] = useState(0)
  const [tableId, setTableId] = useState('')
  const [selected, setSelected] = useState<BojunTable | null>(null)
  const [reload, setReload] = useState(0)
  const [editing, setEditing] = useState(false)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const { metadata, loading, error } = useBojunMetadata(client, datasourceId, [tableId], reload)
  const item = metadata[tableId]
  const [sync, setSync] = useState<BojunSyncRun | null>(null)
  const [operation, setOperation] = useState({ busy: false, error: '', notice: '' })
  const syncing = Boolean(sync && pendingStatuses.includes(sync.status))

  useEffect(() => {
    if (!sync || !datasourceId || !pendingStatuses.includes(sync.status)) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      void getBojunSync(client, datasourceId, sync.syncRunId, controller.signal).then((response) => {
        if (controller.signal.aborted) return
        if (!response.ok) { setOperation({ busy: false, error: response.error, notice: '' }); return }
        setSync(response.data)
        if (['SUCCEEDED', 'SUCCESS'].includes(response.data.status)) setReload((value) => value + 1)
      })
    }, 1500)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [client, datasourceId, sync])

  async function synchronize(single = false) {
    if (!datasourceId || operation.busy || syncing) return
    setOperation({ busy: true, error: '', notice: '' })
    const response = await startBojunSync(client, datasourceId, single ? tableId : undefined)
    if (!response.ok) { setOperation({ busy: false, error: response.error, notice: '' }); return }
    setSync(response.data)
    setOperation({ busy: false, error: '', notice: '' })
    setReload((value) => value + 1)
  }

  const columns = item?.columns ?? []
  const matched = columns.filter((column) => `${column.dbName} ${column.name} ${column.description}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  const pages = Math.max(1, Math.ceil(matched.length / 15))
  const currentPage = Math.min(page, pages)
  const displayColumns = columns.filter((column) => item?.table.displayFields.includes(String(column.id)))

  return <div className={styles.workbench}>
    <div className={styles.toolbar}>
      <label className={styles.field}>Oracle 数据源<select value={datasourceId || ''} onChange={(event) => { setDatasourceId(Number(event.currentTarget.value)); setTableId(''); setSelected(null); setSync(null); setEditing(false); setOperation({ busy: false, error: '', notice: '' }) }}>
        <option value="">请选择数据源</option>{datasources.map((source) => <option value={source.id} key={source.id} disabled={!source.enabled}>{source.name} · {source.code}</option>)}
      </select></label>
      <Button disabled={!datasourceId || operation.busy || syncing} onClick={() => void synchronize()}><Database aria-hidden="true" />{syncing ? '正在同步…' : '同步可用表目录'}</Button>
    </div>
    <div className={styles.catalog}>
      <div className={styles.catalogHeading}><strong>选择要管理的表</strong><span>核验并启用后，才会出现在报表选表列表中</span></div>
      <BojunTableSelect key={`${datasourceId}-${reload}`} client={client} datasourceId={datasourceId} availableOnly={false} value={tableId} selected={item?.table ?? selected ?? undefined} onChange={(table) => { setTableId(String(table.id)); setSelected(table); setSearch(''); setPage(1) }} />
    </div>
    {sync ? <div className={styles.sync} role="status"><StatusTag tone={sync.status === 'FAILED' ? 'danger' : syncing ? 'running' : 'success'}>{statusLabels[sync.status] ?? sync.status}</StatusTag><span>#{sync.syncRunId}</span><strong>{sync.tableCount.toLocaleString('zh-CN')} 张表</strong>{sync.error ? <span>{sync.error}</span> : null}</div> : null}
    {operation.error ? <p className={styles.error} role="alert">{operation.error}</p> : null}
    {operation.notice ? <p className={styles.notice} role="status">{operation.notice}</p> : null}
    {loading ? <FeedbackState kind="loading" title="正在核验表与字段" /> : null}
    {error ? <FeedbackState kind="error" title="字典不可用" description={error} action={<Button onClick={() => setReload((value) => value + 1)}>重新读取</Button>} /> : null}
    {!item && !loading && !error ? <FeedbackState kind="empty" title={datasourceId ? '选择一张表开始配置' : '先选择 Oracle 数据源'} description="首次使用请同步表目录，再选择要用于报表的表。显示字段、允许字段和数据范围在配置弹窗中维护。" /> : null}
    {item ? <section className={styles.tableSection}>
      <div className={styles.heading}>
        <div><p className={styles.eyebrow}>伯俊表字典</p><h3>{item.table.description || item.table.logicalName}</h3><code>{item.table.logicalName}</code></div>
        <div className={styles.actions}><StatusTag tone={item.table.enabled && item.table.validationStatus === 'VALID' ? 'success' : 'warning'}>{item.table.enabled && item.table.validationStatus === 'VALID' ? '报表可用' : item.table.validationStatus === 'VALID' ? '待启用' : '待核验'}</StatusTag><Button disabled={operation.busy || syncing} onClick={() => void synchronize(true)}><RefreshCw aria-hidden="true" />重新核验</Button><Button variant="primary" onClick={() => setEditing(true)}><Settings2 aria-hidden="true" />配置此表</Button></div>
      </div>
      <div className={styles.summary}>
        <div className={styles.displaySummary}><span>默认显示字段</span><strong>{displayColumns.length ? displayColumns.map((column) => column.description || column.dbName).join(' / ') : '未设置，使用记录标识'}</strong><code>{displayColumns.map((column) => column.dbName).join(' + ') || '—'}</code></div>
        <div><span>记录标识</span><strong>{item.table.primaryKeys.map((id) => columns.find((column) => String(column.id) === id)?.dbName ?? `#${id}`).join(' + ') || '尚未核验'}</strong></div>
        <div><span>字段数量</span><strong className={styles.number}>{columns.length.toLocaleString('zh-CN')}</strong></div>
        <div><span>查询范围</span><strong>{item.table.dataScope?.mode === 'GLOBAL_APPROVED' ? '已批准全部记录' : item.table.dataScope?.mode === 'SCOPE_COLUMN' ? '按用户限制范围' : '待配置'}</strong></div>
      </div>
      {item.table.validationReason ? <p role="alert" className={styles.error}>{item.table.validationReason}</p> : null}
      <div className={styles.fieldToolbar}><label className={styles.field}>查找字段<input type="search" placeholder="输入业务名称或字段名" value={search} onChange={(event) => { setSearch(event.currentTarget.value); setPage(1) }} /></label><span>{matched.length} 个字段 · 默认显示字段已标记</span></div>
      <DataTable density="compact" minWidth={680} scrollLabel="伯俊字段目录">
        <thead><tr><th scope="col">字段</th><th scope="col">业务名称</th><th scope="col">类型</th><th scope="col">字段用途</th><th scope="col">可用状态</th></tr></thead>
        <tbody>{matched.slice((currentPage - 1) * 15, currentPage * 15).map((column) => <tr key={column.id}><td><code>{column.dbName || column.name}</code></td><td>{column.description || '—'}</td><td>{column.oracleType || '—'}</td><td><div className={styles.badges}>{item.table.displayFields.includes(String(column.id)) ? <StatusTag tone="info" showDot={false}>默认显示</StatusTag> : null}{item.table.primaryKeys.includes(String(column.id)) ? <StatusTag showDot={false}>记录标识</StatusTag> : null}{item.table.searchFields.includes(String(column.id)) ? <StatusTag showDot={false}>可搜索</StatusTag> : null}</div></td><td>{!column.isPhysical ? '虚拟字段，暂不可用' : column.isSelectable ? '物理字段' : '暂不可用'}</td></tr>)}</tbody>
      </DataTable>
      {!matched.length ? <p className={styles.hint}>没有匹配字段，请调整搜索词。</p> : null}
      {pages > 1 ? <PaginationControls page={currentPage} totalPages={pages} onPrevious={() => setPage(currentPage - 1)} onNext={() => setPage(currentPage + 1)} /> : null}
      <p className={styles.hint}>物理映射：{item.table.resolvedTable || '尚未核验'} · 最后同步：{item.table.lastSyncedAt ? new Date(item.table.lastSyncedAt).toLocaleString('zh-CN') : '—'}</p>
    </section> : null}
    {editing && item ? <BojunTableConfigurationDialog client={client} datasourceId={datasourceId} table={item.table} columns={item.columns} onClose={() => setEditing(false)} onSaved={(table) => { setSelected(table); setEditing(false); setReload((value) => value + 1); setOperation({ busy: false, error: '', notice: '表配置已保存，报表选表列表会自动读取最新状态。' }) }} /> : null}
  </div>
}
