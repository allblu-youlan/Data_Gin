import { useState } from 'react'
import { Button, Dialog, FeedbackState } from '../../../ui'
import type { ReportCenterClient } from '../../api'
import { isBojunColumnAllowed, newReportQueryColumn } from '../../bojunQueryConfig'
import type { ReportQuerySpec } from '../../types'
import { BojunTableSelect, type BojunMetadata } from './BojunMetadata'
import type { QueryFieldOption } from './ReportConditionEditor'
import { ReportDefaultFields } from './ReportDefaultFields'
import { ReportRelationEditor } from './ReportRelationEditor'
import { ReportTableFields } from './ReportTableFields'
import { useBojunMetadata } from './useBojunMetadata'
import styles from './ReportQueryDesigner.module.css'

export function ReportTableConfigDialog({ client, datasourceId, initialSpec, aliasId, adding, initialMetadata, parameters, onClose, onSave }: { client: ReportCenterClient; datasourceId: number; initialSpec: ReportQuerySpec; aliasId: string; adding: boolean; initialMetadata: BojunMetadata; parameters: string[]; onClose: () => void; onSave: (spec: ReportQuerySpec) => void }) {
  const [spec, setSpec] = useState(() => structuredClone(initialSpec))
  const [error, setError] = useState('')
  const lookup = useBojunMetadata(client, datasourceId, [spec.base.tableId, ...spec.relations.map((relation) => relation.targetTableId)])
  const metadata = { ...initialMetadata, ...lookup.metadata }
  const aliases = [{ aliasId: spec.base.aliasId, tableId: spec.base.tableId }, ...spec.relations.map((relation) => ({ aliasId: relation.id, tableId: relation.targetTableId }))]
  const fields: QueryFieldOption[] = aliases.flatMap((alias) => (metadata[alias.tableId]?.columns ?? []).filter((column) => isBojunColumnAllowed(metadata[alias.tableId]?.table, column)).map((column) => ({ aliasId: alias.aliasId, columnId: String(column.id), label: `${metadata[alias.tableId]?.table.description || metadata[alias.tableId]?.table.logicalName} → ${column.dbName}`, operators: column.supportedOperators.length ? column.supportedOperators : ['EQ' as const], logicalType: column.logicalType })))
  const relation = spec.relations.find((item) => item.id === aliasId)
  const tableId = relation ? relation.targetTableId : spec.base.tableId
  const item = metadata[tableId]
  const label = relation?.name || item?.table.description || item?.table.logicalName || '表'
  const outputs = spec.columns.filter((column) => column.source.kind === 'BASE_COLUMN' ? aliasId === spec.base.aliasId : column.source.relationId === aliasId)
  function append(columnId: string, header: string) {
    const source = aliasId === spec.base.aliasId ? { kind: 'BASE_COLUMN' as const, columnId } : { kind: 'RELATION_COLUMN' as const, relationId: aliasId, columnId }
    setSpec((current) => ({ ...current, columns: [...current.columns, newReportQueryColumn(source, header)] }))
  }
  function save() {
    if (!item || !item.table.enabled || item.table.validationStatus !== 'VALID') { setError('请选择已启用且核验通过的表。'); return }
    if (relation && (!relation.fromAliasId || !relation.keyBindings.length || relation.keyBindings.some((binding) => !binding.sourceColumnId || !binding.targetColumnId))) { setError('请完整配置关联键的来源字段与匹配字段。'); return }
    const next = relation ? { ...spec, columns: spec.columns.map((column) => column.source.kind === 'RELATION_DISPLAY' && column.source.relationId === aliasId && adding ? { ...column, header: label } : column) } : spec
    onSave(next)
  }
  return <Dialog open className={styles.tableDialog} title={adding ? '加入关联表' : `配置表 · ${label}`} description="选择字段并配置连线，确认后应用到报表。取消将放弃本次修改。" closeLabel="关闭表配置" onClose={onClose} footer={<><span className={styles.dialogCount}>{outputs.length} 个报表列</span><Button onClick={onClose}>取消</Button><Button variant="primary" disabled={lookup.loading || Boolean(lookup.error)} onClick={save}>{adding ? '确认加入' : '应用配置'}</Button></>}>
    <div className={styles.editor}>
      {lookup.loading ? <FeedbackState kind="loading" title="正在读取表字段" /> : null}{lookup.error ? <FeedbackState kind="error" title="字段读取失败" description={lookup.error} /> : null}
      {relation ? <ReportRelationEditor client={client} datasourceId={datasourceId} relation={relation} spec={spec} metadata={metadata} fields={fields} parameters={parameters} tableLocked={!adding} onChange={(next) => setSpec((current) => ({ ...current, relations: current.relations.map((item) => item.id === aliasId ? next : item), columns: next.targetTableId === relation.targetTableId ? current.columns : current.columns.filter((column) => column.source.kind !== 'RELATION_COLUMN' || column.source.relationId !== aliasId) }))} onAppend={append} /> : <>
        <section className={styles.section}><h3>选择表</h3><BojunTableSelect client={client} datasourceId={datasourceId} value={tableId} selected={item?.table} disabled={!adding} label="选择要加入的表" onChange={(table) => setSpec((current) => ({ ...current, base: { tableId: String(table.id), aliasId: 'base' }, columns: [] }))} /></section>
        {item ? <><ReportDefaultFields table={item.table} columns={item.columns} /><ReportTableFields key={item.table.id} table={item.table} columns={item.columns} onAppend={append} /></> : <p className={styles.hint}>先从可用目录中选择一张表，再选择需要的字段。</p>}
      </>}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
    </div>
  </Dialog>
}
