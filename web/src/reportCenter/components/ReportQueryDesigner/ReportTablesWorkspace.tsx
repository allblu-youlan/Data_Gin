import { useState } from 'react'
import { ArrowRight, Columns3, Link2, Plus, Settings2, Table2, Trash2, X } from 'lucide-react'
import { Button, Dialog, FeedbackState } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { newReportQueryColumn } from '../../bojunQueryConfig'
import type { ReportQuerySpec } from '../../types'
import type { BojunMetadata } from './BojunMetadata'
import reportDesign from '../../reportDesign.module.css'
import { ReportRelationMap } from './ReportRelationMap'
import { ReportTableFields } from './ReportTableFields'
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
  const selectedCounts = columns.reduce<Record<string, number>>((all, column) => { if (column.source.kind !== 'RELATION_DISPLAY') all[column.source.columnId] = (all[column.source.columnId] ?? 0) + 1; return all }, {})
  function append(columnId: string, header: string) {
    if (!active) return
    const source = active.aliasId === spec.base.aliasId ? { kind: 'BASE_COLUMN' as const, columnId } : { kind: 'RELATION_COLUMN' as const, relationId: active.aliasId, columnId }
    onChange({ ...spec, columns: [...spec.columns, newReportQueryColumn(source, header)] })
  }
  return <section className={styles.tableWorkspace} aria-label="关联表工作台">
    {!tables.length ? <div className={styles.workspaceEmpty}><div className={styles.emptyDiagram} aria-hidden="true"><Table2 /><span /><Plus /><span /><Table2 /></div><span className={styles.eyebrow}>START WITH DATA</span><h4>从业务数据开始搭建</h4><p>选择一张伯俊业务表，字典会带入默认显示字段。<br />随后可继续连接其它表，组合成需要的报表。</p><Button variant="primary" disabled={!datasourceId} onClick={() => add()}><Plus aria-hidden="true" data-icon="inline-start" />选择可用表</Button></div> : <>
      <ReportRelationMap spec={spec} metadata={metadata} tables={tables} activeAlias={active?.aliasId} label={label} onSelect={setSelectedAlias} onAdd={() => add()} canAdd={Boolean(datasourceId) && spec.relations.length < 8} />
      <div className={styles.tableDetail}>
        <header className={styles.tableSummaryHeader}><div className={styles.tableTitle}><span className={styles.tableDetailIcon}><Table2 aria-hidden="true" /></span><div><h4>{active ? label(active.aliasId) : '表'}</h4><code>{item?.table.logicalName}</code></div></div><div className={styles.actions}><Button onClick={() => active && setEdit({ aliasId: active.aliasId, adding: false, spec })}><Settings2 aria-hidden="true" data-icon="inline-start" />配置表</Button><Button aria-label={`移除 ${active ? label(active.aliasId) : '表'}`} onClick={() => active && setRemoveAlias(active.aliasId)}><Trash2 aria-hidden="true" /></Button></div></header>
        <div className={styles.tableContext}>
          {item ? <ReportDefaultFields table={item.table} columns={item.columns} selected={relation?.displayColumnIds} /> : null}
          <section className={styles.connectionSummary}><div className={styles.connectionTitle}><Link2 aria-hidden="true" /><strong>{relation ? '记录连接' : '业务表'}</strong></div>{relation ? <><p>{label(relation.fromAliasId)}<ArrowRight aria-hidden="true" />{label(relation.id)}</p><div className={styles.keySummary}>{relation.keyBindings.map((binding, index) => { const source = tables.find((table) => table.aliasId === relation.fromAliasId); return <span key={index}><code>{fieldName(source?.tableId ?? '', binding.sourceColumnId)}</code><span>=</span><code>{fieldName(relation.targetTableId, binding.targetColumnId)}</code></span> })}</div>{!relation.keyBindings.length ? <p className={styles.error}>关联键尚未配置，请打开“配置表”补齐。</p> : null}</> : <p className={styles.hint}>已加入报表。可连接其它业务表，补充关联信息。</p>}</section>
        </div>
        {item && (!item.table.enabled || item.table.validationStatus !== 'VALID') ? <FeedbackState kind="error" title="这张表当前不可用" description="请更换业务表，或重新读取字典。" /> : null}
        <div className={styles.fieldWorkbench}>
          <div className={styles.fieldWorkbenchMain}>{item ? <ReportTableFields key={active?.aliasId} table={item.table} columns={item.columns} selectedCounts={selectedCounts} onAppend={append} /> : <FeedbackState kind="loading" title="正在读取业务字段…" />}</div>
          <section className={styles.outputQueue} aria-label="已选报表列"><header><div><Columns3 aria-hidden="true" /><h4>输出字段</h4></div><span>{columns.length}</span></header><p>来自当前业务表</p>{columns.length ? <ol>{columns.map((column, index) => <li key={column.fieldId}><span className={styles.outputOrdinal}>{String(index + 1).padStart(2, '0')}</span><div><strong>{column.header || '未命名列'}</strong><small>{column.source.kind === 'RELATION_DISPLAY' ? '默认显示值' : fieldName(active?.tableId ?? '', column.source.columnId)}</small></div><Button aria-label={`移除输出字段 ${column.header || '未命名列'}`} onClick={() => onChange({ ...spec, columns: spec.columns.filter((item) => item.fieldId !== column.fieldId) })}><X aria-hidden="true" /></Button></li>)}</ol> : <div className={styles.outputEmpty}><Columns3 aria-hidden="true" /><span>从左侧加入需要的字段</span></div>}<footer>列名称、顺序和导出范围<br />可在“列与导出”步骤中调整。</footer></section>
        </div>
      </div>
    </>}
    {edit ? <ReportTableConfigDialog client={client} datasourceId={datasourceId} initialSpec={edit.spec} aliasId={edit.aliasId} adding={edit.adding} initialMetadata={metadata} parameters={parameters} onClose={() => setEdit(null)} onSave={(query) => { onChange(query); setSelectedAlias(edit.aliasId); setEdit(null) }} /> : null}
    <Dialog className={reportDesign.surface} open={Boolean(removed)} role="alertdialog" title="移除这张表？" description={removeAlias ? `移除“${label(removeAlias)}”将同时清理依赖它的配置。` : undefined} onClose={() => setRemoveAlias('')} footer={<><Button onClick={() => setRemoveAlias('')}>保留表</Button><Button variant="danger" onClick={() => { if (removed) onChange(removed.query); setSelectedAlias(spec.base.aliasId); setRemoveAlias('') }}>确认移除</Button></>}>
      {removed ? <p className={styles.removalNotice}>将移除 {removed.tableCount} 张表、{removed.columnCount} 个报表列，以及引用这些表的筛选和排序。其他表的字段保持不变。</p> : null}
    </Dialog>
  </section>
}
