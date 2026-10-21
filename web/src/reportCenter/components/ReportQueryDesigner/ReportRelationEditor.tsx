import { useEffect } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { Button, StatusTag } from '../../../ui'
import { isBojunColumnAllowed } from '../../bojunQueryConfig'
import type { ReportCenterClient } from '../../api'
import type { ReportQueryRelation, ReportQuerySpec } from '../../types'
import { BojunTableSelect, type BojunMetadata } from './BojunMetadata'
import { ReportConditionEditor, type QueryFieldOption } from './ReportConditionEditor'
import { ReportDefaultFields } from './ReportDefaultFields'
import { ReportTableFields } from './ReportTableFields'
import { inheritDictionaryRelationKeys, relationSourceAliases } from './tableWorkspace'
import styles from './ReportQueryDesigner.module.css'

export function ReportRelationEditor({ client, datasourceId, relation, spec, metadata, fields, parameters, tableLocked = false, showTableSelection = true, showFields = true, onChange, onAppend }: { client: ReportCenterClient; datasourceId: number; relation: ReportQueryRelation; spec: ReportQuerySpec; metadata: BojunMetadata; fields: QueryFieldOption[]; parameters: string[]; tableLocked?: boolean; showTableSelection?: boolean; showFields?: boolean; onChange: (relation: ReportQueryRelation) => void; onAppend: (columnId: string, header: string) => void }) {
  const tableId = (alias: string) => alias === spec.base.aliasId ? spec.base.tableId : spec.relations.find((item) => item.id === alias)?.targetTableId ?? ''
  const name = (alias: string) => { const item = metadata[tableId(alias)]; return item?.table.description || item?.table.logicalName || spec.relations.find((item) => item.id === alias)?.name || '已加入的表' }
  const source = metadata[tableId(relation.fromAliasId)]
  const sourceColumns = source?.columns.filter((column) => isBojunColumnAllowed(source.table, column)) ?? []
  const target = metadata[relation.targetTableId]
  useEffect(() => {
    const inherited = inheritDictionaryRelationKeys(relation, source, target)
    if (inherited !== relation) onChange(inherited)
  }, [onChange, relation, source, target])
  const selectedDisplay = relation.displayColumnIds.length ? relation.displayColumnIds : target?.table.displayFields.length ? target.table.displayFields : target?.table.primaryKeys ?? []
  const allowedAliases = relationSourceAliases(spec, relation.id)
  return <div className={styles.editor}>
    <section className={styles.section}>
      <div className={styles.header}><h3>表与连线</h3><StatusTag tone="info" showDot={false}>保留来源记录</StatusTag></div>
      <div className={styles.form}>
        {showTableSelection ? <div className={`${styles.field} ${styles.wide}`}><span>选择表</span><BojunTableSelect client={client} datasourceId={datasourceId} value={relation.targetTableId} selected={target?.table} disabled={tableLocked} label="选择要关联的表" onChange={(table) => onChange({ ...relation, name: table.description || table.logicalName, targetTableId: String(table.id), displayColumnIds: table.displayFields, detailColumnIds: table.displayFields.filter((id) => table.config?.detailColumns == null || table.config.detailColumns.includes(id)), keyBindings: table.primaryKeys.map((targetColumnId, index) => ({ sourceColumnId: relation.keyBindings[index]?.sourceColumnId ?? '', targetColumnId })) })} /></div> : null}
        <label className={styles.field}>显示名称<input value={relation.name} placeholder={target?.table.description || '用于识别这张表'} onChange={(event) => onChange({ ...relation, name: event.currentTarget.value })} /></label>
        <label className={styles.field}>从哪张表连接<select value={relation.fromAliasId} onChange={(event) => onChange({ ...relation, fromAliasId: event.currentTarget.value, keyBindings: relation.keyBindings.map((binding) => ({ ...binding, sourceColumnId: '' })) })}>{allowedAliases.map((alias) => <option key={alias} value={alias}>{name(alias)}</option>)}</select></label>
      </div>
      <p className={styles.hint}>没有匹配记录时，仍保留“{name(relation.fromAliasId)}”的报表记录。优先继承伯俊字典中的完整关联键；需要时可调整字段。</p>
      <fieldset className={styles.keyFieldset}><legend>关联键 · {relation.keyBindings.length} 个分量</legend>{relation.keyBindings.map((binding, index) => <div className={styles.keyBinding} key={index}>
        <label className={styles.field}>来源字段<select aria-label={`第 ${index + 1} 个来源字段`} value={binding.sourceColumnId} onChange={(event) => { const sourceColumnId = event.currentTarget.value; onChange({ ...relation, keyBindings: relation.keyBindings.map((item, itemIndex) => itemIndex === index ? { ...item, sourceColumnId } : item) }) }}><option value="">选择来源字段</option>{sourceColumns.map((column) => <option key={column.id} value={String(column.id)}>{column.description || column.dbName} · {column.dbName}</option>)}</select></label>
        <span className={styles.bindingEquals} aria-hidden="true">=</span>
        <label className={styles.field}>匹配字段<select aria-label={`第 ${index + 1} 个匹配字段`} value={binding.targetColumnId} onChange={(event) => { const targetColumnId = event.currentTarget.value; onChange({ ...relation, keyBindings: relation.keyBindings.map((item, itemIndex) => itemIndex === index ? { ...item, targetColumnId } : item) }) }}><option value="">选择匹配字段</option>{target?.columns.filter((column) => isBojunColumnAllowed(target.table, column)).map((column) => <option key={column.id} value={String(column.id)}>{column.description || column.dbName} · {column.dbName}</option>)}</select></label>
        <Button aria-label={`删除第 ${index + 1} 个键分量`} onClick={() => onChange({ ...relation, keyBindings: relation.keyBindings.filter((_, itemIndex) => itemIndex !== index) })}><Trash2 aria-hidden="true" /></Button>
      </div>)}<Button disabled={!target} onClick={() => onChange({ ...relation, keyBindings: [...relation.keyBindings, { sourceColumnId: '', targetColumnId: '' }] })}><Plus aria-hidden="true" data-icon="inline-start" />添加键分量</Button></fieldset>
    </section>
    {showFields && target ? <><ReportDefaultFields table={target.table} columns={target.columns} selected={selectedDisplay} /><ReportTableFields key={target.table.id} table={target.table} columns={target.columns} displayIds={selectedDisplay} detailIds={relation.detailColumnIds} onDisplayChange={(displayColumnIds) => onChange({ ...relation, displayColumnIds })} onDetailChange={(detailColumnIds) => onChange({ ...relation, detailColumnIds })} onAppend={onAppend} /></> : null}
    <details className={styles.advanced}><summary>取值限制 · 只限制匹配记录</summary><p className={styles.hint}>这些条件控制关联表取值，不会移除来源记录。</p><ReportConditionEditor value={relation.matchConditions} fields={fields.filter((field) => allowedAliases.includes(field.aliasId) || field.aliasId === relation.id)} parameters={parameters} onChange={(matchConditions) => onChange({ ...relation, matchConditions })} /></details>
  </div>
}
