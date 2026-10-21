import { useState } from 'react'
import { Link2, Network, Plus, Search, Settings2, Table2, Trash2 } from 'lucide-react'
import { Button, Dialog, FeedbackState } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { newReportQueryColumn } from '../../bojunQueryConfig'
import type { ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import reportDesign from '../../reportDesign.module.css'
import { ReportWorkspaceFields } from './ReportWorkspaceFields'
import { ReportWorkspaceOutput } from './ReportWorkspaceOutput'
import { ReportTableConfigDialog } from './ReportTableConfigDialog'
import { ReportRelationsDialog } from './ReportRelationsDialog'
import { newWorkspaceRelation, removeReportTables } from './tableWorkspace'
import styles from './ReportTablesWorkspace.module.css'

type TableEdit = { aliasId: string; adding: boolean; spec: ReportQuerySpec }

export function ReportTablesWorkspace({ client, datasourceId, spec, metadata, parameters, onChange }: { client: ReportCenterClient; datasourceId: number; spec: ReportQuerySpec; metadata: BojunMetadata; parameters: string[]; onChange: (spec: ReportQuerySpec) => void }) {
  const [selectedAlias, setSelectedAlias] = useState(spec.base.aliasId)
  const [search, setSearch] = useState('')
  const [edit, setEdit] = useState<TableEdit | null>(null)
  const [removeAlias, setRemoveAlias] = useState('')
  const [relationsOpen, setRelationsOpen] = useState(false)
  const tables = [...(spec.base.tableId ? [{ aliasId: spec.base.aliasId, tableId: spec.base.tableId }] : []), ...spec.relations.map((relation) => ({ aliasId: relation.id, tableId: relation.targetTableId }))]
  const active = tables.find((table) => table.aliasId === selectedAlias) ?? tables[0]
  const item = metadata[active?.tableId ?? '']
  const relation = spec.relations.find((relation) => relation.id === active?.aliasId)
  const label = (aliasId: string) => {
    const table = tables.find((table) => table.aliasId === aliasId)
    const detail = metadata[table?.tableId ?? '']
    return spec.relations.find((relation) => relation.id === aliasId)?.name || detail?.table.description || detail?.table.logicalName || '正在读取表…'
  }
  const selectedCounts = spec.columns.reduce<Record<string, number>>((all, column) => {
    const source = column.source
    if (source.kind === 'BASE_COLUMN' && active?.aliasId === spec.base.aliasId || source.kind === 'RELATION_COLUMN' && source.relationId === active?.aliasId) all[source.columnId] = (all[source.columnId] ?? 0) + 1
    return all
  }, {})
  const shown = tables.filter((table) => `${label(table.aliasId)} ${metadata[table.tableId]?.table.logicalName || ''}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  const removed = removeAlias ? removeReportTables(spec, removeAlias) : null
  function add(fromAliasId = active?.aliasId ?? spec.base.aliasId) {
    if (!spec.base.tableId) { setEdit({ aliasId: spec.base.aliasId, adding: true, spec }); return }
    const id = `rel_${crypto.randomUUID().replace(/-/g, '').slice(0, 12)}`
    setEdit({ aliasId: id, adding: true, spec: { ...spec, relations: [...spec.relations, newWorkspaceRelation(id, fromAliasId)], columns: [...spec.columns, newReportQueryColumn({ kind: 'RELATION_DISPLAY', relationId: id }, '关联显示')] } })
  }
  function toggleField(columnId: string, header: string, checked: boolean) {
    if (!active) return
    const source = active.aliasId === spec.base.aliasId ? { kind: 'BASE_COLUMN' as const, columnId } : { kind: 'RELATION_COLUMN' as const, relationId: active.aliasId, columnId }
    const columns = checked ? [...spec.columns, newReportQueryColumn(source, header)] : spec.columns.filter((column) => column.source.kind === 'RELATION_DISPLAY' || (column.source.kind === 'BASE_COLUMN' ? spec.base.aliasId : column.source.relationId) !== active.aliasId || column.source.columnId !== columnId)
    onChange({ ...spec, columns })
  }
  return <section className={styles.workspace} aria-label="报表数据与字段工作台">
    <div className={styles.panes}>
      <aside className={styles.catalog} aria-label="已加入的业务表">
        <header className={styles.paneHeading}><h3><Table2 aria-hidden="true" />业务表目录 <span>{tables.length}</span></h3><Button aria-label="选择可用表" disabled={!datasourceId || spec.relations.length >= 8} onClick={() => add()}><Plus aria-hidden="true" /></Button></header>
        <label className={styles.search}><Search aria-hidden="true" /><input name="report-table-search" autoComplete="off" aria-label="搜索已加入的表" placeholder="搜索已加入的表…" value={search} onChange={(event) => setSearch(event.currentTarget.value)} /></label>
        <div className={styles.catalogLabel}><span>已加入报表</span><span>{shown.length} 张</span></div>
        <div className={styles.tableList}>{shown.map((table, index) => <button type="button" key={table.aliasId} className={styles.tableRow} data-active={active?.aliasId === table.aliasId} aria-pressed={active?.aliasId === table.aliasId} onClick={() => setSelectedAlias(table.aliasId)}><span className={styles.tableIcon}><Table2 aria-hidden="true" /></span><span className={styles.tableCopy}><strong>{label(table.aliasId)}</strong><code>{metadata[table.tableId]?.table.logicalName || '读取字典中…'}</code></span><span className={styles.tableOrdinal}>{String(index + 1).padStart(2, '0')}</span></button>)}{!shown.length ? <p className={styles.emptyHint}>{tables.length ? '没有匹配的已加入表' : '从可用表目录中加入业务数据'}</p> : null}</div>
        <Button className={styles.addTable} disabled={!datasourceId || spec.relations.length >= 8} onClick={() => add()}><Plus aria-hidden="true" />选择可用表</Button>
        <footer className={styles.catalogFooter}><span aria-hidden="true" />伯俊字典自动继承</footer>
      </aside>
      <section className={styles.fieldPane} aria-label="当前表字段">
        {active ? <>
          <header className={styles.fieldHeading}><div><h3>{label(active.aliasId)}</h3><code>{item?.table.logicalName || '正在读取字典…'}</code></div><div className={styles.tools}><Button onClick={() => setRelationsOpen(true)}><Network aria-hidden="true" />查看关系</Button><Button aria-label={`配置 ${label(active.aliasId)}`} onClick={() => setEdit({ aliasId: active.aliasId, adding: false, spec })}><Settings2 aria-hidden="true" />配置</Button><Button aria-label={`移除 ${label(active.aliasId)}`} onClick={() => setRemoveAlias(active.aliasId)}><Trash2 aria-hidden="true" /></Button></div></header>
          <div className={styles.connectionLine}><Link2 aria-hidden="true" />{relation ? <span>{label(relation.fromAliasId)}<span aria-hidden="true"> → </span><strong>{label(active.aliasId)}</strong></span> : <span>已加入报表，可继续关联其他业务表</span>}<span className={styles.inherited}>沿用字典定义</span></div>
          {item ? <ReportWorkspaceFields key={active.aliasId} table={item.table} columns={item.columns} selectedCounts={selectedCounts} onToggle={toggleField} /> : <FeedbackState kind="loading" title="正在读取业务字段…" />}
        </> : <div className={styles.emptyWorkspace}><Table2 aria-hidden="true" /><h3>选择业务数据，开始搭建报表</h3><p>从可用表目录中选择表和字段，默认显示与关联定义自动带入。</p><Button variant="primary" disabled={!datasourceId} onClick={() => add()}><Plus aria-hidden="true" />打开可用表目录</Button></div>}
      </section>
      <ReportWorkspaceOutput spec={spec} metadata={metadata} activeAlias={active?.aliasId} label={label} onChange={onChange} />
    </div>
    <ReportWorkspaceOutput spec={spec} metadata={metadata} label={label} onChange={onChange} preview />
    {relationsOpen ? <ReportRelationsDialog spec={spec} metadata={metadata} initialAlias={active?.aliasId} onClose={() => setRelationsOpen(false)} /> : null}
    {edit ? <ReportTableConfigDialog client={client} datasourceId={datasourceId} initialSpec={edit.spec} aliasId={edit.aliasId} adding={edit.adding} initialMetadata={metadata} parameters={parameters} onClose={() => setEdit(null)} onSave={(query) => { onChange(query); setSelectedAlias(edit.aliasId); setEdit(null) }} /> : null}
    <Dialog className={reportDesign.surface} open={Boolean(removed)} role="alertdialog" title="移除这张表？" description={removeAlias ? `移除“${label(removeAlias)}”将同时清理依赖它的配置。` : undefined} onClose={() => setRemoveAlias('')} footer={<><Button onClick={() => setRemoveAlias('')}>保留表</Button><Button variant="danger" onClick={() => { if (removed) onChange(removed.query); setSelectedAlias(spec.base.aliasId); setRemoveAlias('') }}>确认移除</Button></>}>
      {removed ? <p>将移除 {removed.tableCount} 张表、{removed.columnCount} 个报表列，以及引用这些表的筛选和排序。</p> : null}
    </Dialog>
  </section>
}
