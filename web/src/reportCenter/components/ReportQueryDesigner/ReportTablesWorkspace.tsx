import { useState } from 'react'
import { ArrowRight, Link2, Plus, Settings2, Table2, Trash2 } from 'lucide-react'
import { Button, Dialog, FeedbackState, StatusTag } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { newReportQueryColumn } from '../../bojunQueryConfig'
import type { ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import { ReportDefaultFields } from './ReportDefaultFields'
import { ReportTableConfigDialog } from './ReportTableConfigDialog'
import { newWorkspaceRelation, removeReportTables } from './tableWorkspace'
import styles from './ReportQueryDesigner.module.css'

type TableEdit = { aliasId: string; adding: boolean; spec: ReportQuerySpec }

export function ReportTablesWorkspace({ client, datasourceId, spec, metadata, parameters, onChange }: { client: ReportCenterClient; datasourceId: number; spec: ReportQuerySpec; metadata: BojunMetadata; parameters: string[]; onChange: (spec: ReportQuerySpec) => void }) {
  const [selectedAlias, setSelectedAlias] = useState(spec.base.aliasId)
  const [edit, setEdit] = useState<TableEdit | null>(null)
  const [removeAlias, setRemoveAlias] = useState('')
  const tables = [...(spec.base.tableId ? [{ aliasId: spec.base.aliasId, tableId: spec.base.tableId }] : []), ...spec.relations.map((relation) => ({ aliasId: relation.id, tableId: relation.targetTableId }))]
  const active = tables.find((table) => table.aliasId === selectedAlias) ?? tables[0]
  const item = metadata[active?.tableId ?? '']
  const relation = spec.relations.find((relation) => relation.id === active?.aliasId)
  const label = (aliasId: string) => { const table = tables.find((table) => table.aliasId === aliasId); const detail = metadata[table?.tableId ?? '']; return spec.relations.find((relation) => relation.id === aliasId)?.name || detail?.table.description || detail?.table.logicalName || '待配置的表' }
  const columns = spec.columns.filter((column) => (column.source.kind === 'BASE_COLUMN' ? spec.base.aliasId : column.source.relationId) === active?.aliasId)
  const removed = removeAlias ? removeReportTables(spec, removeAlias) : null
  const fieldName = (tableId: string, id: string) => { const column = metadata[tableId]?.columns.find((column) => String(column.id) === id); return column?.description || column?.dbName || `字段 #${id}` }
  function add(fromAliasId = active?.aliasId ?? spec.base.aliasId) {
    if (!spec.base.tableId) { setEdit({ aliasId: spec.base.aliasId, adding: true, spec }); return }
    const id = `rel_${crypto.randomUUID().replace(/-/g, '').slice(0, 12)}`
    setEdit({ aliasId: id, adding: true, spec: { ...spec, relations: [...spec.relations, newWorkspaceRelation(id, fromAliasId)], columns: [...spec.columns, newReportQueryColumn({ kind: 'RELATION_DISPLAY', relationId: id }, '关联显示')] } })
  }
  return <section className={styles.tableWorkspace} aria-label="关联表工作台">
    <header className={styles.workspaceHeader}><div><h3>关联表</h3><p>加入业务表，选择字段，再用关联键连接记录。</p></div><Button variant="primary" disabled={!datasourceId || spec.relations.length >= 8} onClick={() => add()}><Plus aria-hidden="true" data-icon="inline-start" />加入表</Button></header>
    {!tables.length ? <FeedbackState kind="empty" title="从一张业务表开始" description="直接读取伯俊字典中的可用表。加入后，可随时连接其他表并选择报表字段。" action={<Button disabled={!datasourceId} onClick={() => add()}>选择可用表</Button>} /> : <div className={styles.tableWorkspaceBody}>
      <nav className={styles.tableNavigation} aria-label="已加入的表"><div className={styles.tableNavHeading}><span>已加入的表</span><strong>{tables.length}</strong></div>{tables.map((table, index) => { const detail = metadata[table.tableId]; const available = detail?.table.enabled && detail.table.validationStatus === 'VALID'; return <div className={styles.tableNavItem} data-active={active?.aliasId === table.aliasId} key={table.aliasId}><button className={styles.tableNavButton} type="button" aria-pressed={active?.aliasId === table.aliasId} onClick={() => setSelectedAlias(table.aliasId)}><span className={styles.tableOrdinal}>{String(index + 1).padStart(2, '0')}</span><span><strong>{label(table.aliasId)}</strong><code>{detail?.table.logicalName || '尚未选择表'}</code></span>{detail ? <StatusTag tone={available ? 'success' : 'warning'} showDot={false}>{available ? '可用' : '不可用'}</StatusTag> : null}</button><Button aria-label={`配置 ${label(table.aliasId)}`} onClick={() => { setSelectedAlias(table.aliasId); setEdit({ aliasId: table.aliasId, adding: false, spec }) }}><Settings2 aria-hidden="true" /></Button></div> })}</nav>
      <div className={styles.tableSummary}>
        <header className={styles.tableSummaryHeader}><div className={styles.tableTitle}><Table2 aria-hidden="true" /><div><h4>{active ? label(active.aliasId) : '表'}</h4><code>{item?.table.logicalName}</code></div></div><div className={styles.actions}><Button onClick={() => active && setEdit({ aliasId: active.aliasId, adding: false, spec })}><Settings2 aria-hidden="true" data-icon="inline-start" />配置表</Button><Button disabled={spec.relations.length >= 8} onClick={() => add()}><Link2 aria-hidden="true" data-icon="inline-start" />连接另一张表</Button></div></header>
        {item ? <ReportDefaultFields table={item.table} columns={item.columns} selected={relation?.displayColumnIds} /> : null}
        {item && (!item.table.enabled || item.table.validationStatus !== 'VALID') ? <FeedbackState kind="error" title="这张表当前不可用" description="请先在伯俊字典中核验并启用，或移除后重新选择可用表。" /> : null}
        {relation ? <section className={styles.connectionSummary}><div className={styles.connectionTitle}><Link2 aria-hidden="true" /><strong>记录连接</strong><StatusTag showDot={false}>保留来源记录</StatusTag></div><p>{label(relation.fromAliasId)}<ArrowRight aria-hidden="true" />{label(relation.id)}</p><ul>{relation.keyBindings.map((binding, index) => { const source = tables.find((table) => table.aliasId === relation.fromAliasId); return <li key={index}><span>{fieldName(source?.tableId ?? '', binding.sourceColumnId)}</span><code>=</code><span>{fieldName(relation.targetTableId, binding.targetColumnId)}</span></li> })}</ul>{!relation.keyBindings.length ? <p className={styles.error}>关联键尚未配置，请打开“配置表”补齐。</p> : null}</section> : <p className={styles.hint}>已加入报表。可从这张表继续连接其他业务表。</p>}
        <section className={styles.columnSummary}><div className={styles.header}><h4>已选报表列</h4><StatusTag showDot={false}>{columns.length} 列</StatusTag></div>{columns.length ? <div className={styles.badges}>{columns.map((column) => <StatusTag showDot={false} key={column.fieldId}>{column.header || '未命名列'}</StatusTag>)}</div> : <p className={styles.hint}>打开“配置表”，将需要的字段加入报表。</p>}</section>
        <footer className={styles.tableSummaryFooter}><span>字段取值、显示名称和详情可见范围在配置弹窗内维护。</span><Button variant="danger" onClick={() => active && setRemoveAlias(active.aliasId)}><Trash2 aria-hidden="true" data-icon="inline-start" />移除表</Button></footer>
      </div>
    </div>}
    {edit ? <ReportTableConfigDialog client={client} datasourceId={datasourceId} initialSpec={edit.spec} aliasId={edit.aliasId} adding={edit.adding} initialMetadata={metadata} parameters={parameters} onClose={() => setEdit(null)} onSave={(query) => { onChange(query); setSelectedAlias(edit.aliasId); setEdit(null) }} /> : null}
    <Dialog open={Boolean(removed)} role="alertdialog" title="移除这张表？" description={removeAlias ? `移除“${label(removeAlias)}”将同时清理依赖它的配置。` : undefined} onClose={() => setRemoveAlias('')} footer={<><Button onClick={() => setRemoveAlias('')}>保留表</Button><Button variant="danger" onClick={() => { if (removed) onChange(removed.query); setSelectedAlias(spec.base.aliasId); setRemoveAlias('') }}>确认移除</Button></>}>
      {removed ? <p className={styles.removalNotice}>将移除 {removed.tableCount} 张表、{removed.columnCount} 个报表列，以及引用这些表的筛选和排序。其他表的字段保持不变。</p> : null}
    </Dialog>
  </section>
}
