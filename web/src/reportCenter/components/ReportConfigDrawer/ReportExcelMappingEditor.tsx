import { ArrowDown, ArrowUp, Info, Plus, Trash2 } from 'lucide-react'
import Select from 'antd/es/select'
import { useMemo, useState } from 'react'
import { appendReportColumnFromResultSchema, countReportColumnsMissingFromResultSchema, moveReportExportColumn, replaceExcelMappingFieldWithResultSchema, reportExportValueTypeOptions } from '../../refCursorConfig'
import type { ReportColumn, ReportResultTableColumn } from '../../types'
import { ReportFieldDetailDrawer } from '../ReportFieldDetailDrawer/ReportFieldDetailDrawer'
import styles from './ReportExcelMappingEditor.module.css'

export function ReportExcelMappingEditor({ result, columns, schema, schemaState, onChange, onRetry }: {
  result: { tableOwner: string; tableName: string }
  columns: ReportColumn[]
  schema: ReportResultTableColumn[] | null
  schemaState: ExcelMappingSchemaState
  onChange: (columns: ReportColumn[]) => void
  onRetry: () => void
}) {
  const [detailColumn, setDetailColumn] = useState<ReportColumn | null>(null)
  const orderedColumns = useMemo(() => [...columns].sort((left, right) => {
    const leftExportable = left.exportVisible && left.exportAllowed
    const rightExportable = right.exportVisible && right.exportAllowed
    if (leftExportable !== rightExportable) return leftExportable ? -1 : 1
    return left.exportOrder - right.exportOrder || left.displayOrder - right.displayOrder
  }), [columns])
  const exportedColumns = useMemo(() => orderedColumns.filter((column) => column.exportVisible && column.exportAllowed), [orderedColumns])
  const exportIndexByField = useMemo(() => new Map(exportedColumns.map((column, index) => [column.fieldId, index])), [exportedColumns])
  const usedFields = useMemo(() => new Set(columns.map((column) => column.databaseColumn.toUpperCase())), [columns])
  const availableFields = useMemo(() => schema?.filter((column) => !usedFields.has(column.name.toUpperCase())) ?? [], [schema, usedFields])
  const invalidCount = schema ? countReportColumnsMissingFromResultSchema(columns, schema) : 0

  function add() {
    const column = availableFields[0]
    if (!column) return
    onChange(appendReportColumnFromResultSchema(columns, column))
  }

  function updateField(currentField: string, nextField: string) {
    if (!schema) return
    onChange(replaceExcelMappingFieldWithResultSchema(columns, currentField, nextField, schema))
  }

  function updateHeader(fieldId: string, excelHeader: string) {
    onChange(columns.map((column) => column.fieldId === fieldId ? { ...column, previewHeader: excelHeader, excelHeader } : column))
  }

  function updateValueType(fieldId: string, valueType: string) {
    onChange(columns.map((column) => column.fieldId === fieldId ? { ...column, valueType } : column))
  }

  function toggleExport(fieldId: string, exportVisible: boolean) {
    const exportOrder = columns.reduce((maximum, item) => Math.max(maximum, item.exportOrder), -1) + 1
    onChange(columns.map((column) => column.fieldId === fieldId ? { ...column, exportVisible, ...(exportVisible ? { exportOrder } : {}) } : column))
  }

  return <div className={styles.editor}>
    <div className={styles.tableHeader}><div><h3>Excel 字段映射</h3><p>可调整导出顺序、表头和展示 / 导出类型；小数在 Excel 中固定显示两位。</p></div><button type="button" onClick={add} disabled={schemaState.status === 'loading' || availableFields.length === 0}><Plus aria-hidden="true" />{availableFields.length === 0 && schema ? '字段已全部添加' : '新增映射'}</button></div>
    {schemaState.status === 'loading' ? <p className={styles.status} role="status">正在读取 Oracle 结果表字段…</p> : null}
    {invalidCount > 0 ? <p className={styles.warning} role="alert">有 {invalidCount} 个旧字段不属于当前结果表，请重新选择或删除后再保存。</p> : null}
    {schemaState.status === 'error' ? <div className={styles.errorRow} role="alert"><p className={styles.error}>{schemaState.error}</p><button type="button" onClick={onRetry}>重新读取</button></div> : null}
    {!result.tableOwner || !result.tableName ? <p className={styles.empty}>请先在“存储过程”中选择 Oracle 结果表。</p> : null}
    <div className={styles.rows}>
      {orderedColumns.map((column, index) => { const databaseColumn = column.databaseColumn; const fieldValid = schema?.some((item) => item.name.toUpperCase() === databaseColumn.toUpperCase()) === true; const exportIndex = exportIndexByField.get(column.fieldId) ?? -1; return <div className={styles.row} key={column.fieldId}>
        <label><span>Oracle 结果表字段</span><Select className={`${styles.fieldSelect} ${schema && !fieldValid ? styles.invalid : ''}`} aria-invalid={!schema || !fieldValid} aria-label={`第 ${index + 1} 列 Oracle 结果表字段`} disabled={!schema || schemaState.status === 'loading'} loading={schemaState.status === 'loading'} optionFilterProp="label" showSearch value={databaseColumn} options={[...(schema && !fieldValid ? [{ value: databaseColumn, label: `${databaseColumn} · 字段已失效`, disabled: true }] : []), ...(schema ?? []).map((item) => ({ value: item.name, label: `${item.name} · ${item.oracleType}`, disabled: item.name.toUpperCase() !== databaseColumn.toUpperCase() && usedFields.has(item.name.toUpperCase()) }))]} notFoundContent={schemaState.status === 'loading' ? '正在读取字段…' : '没有可选字段'} onChange={(value) => updateField(databaseColumn, value)} /></label>
        <label><span>Excel 表头</span><input disabled={!schema} value={column.excelHeader} onChange={(event) => updateHeader(column.fieldId, event.currentTarget.value)} /></label>
        <label><span>展示 / 导出类型</span><Select className={styles.typeSelect} aria-label={`${databaseColumn} 展示和导出类型`} disabled={!schema || !fieldValid} value={column.valueType} options={reportExportValueTypeOptions(column.sourceOracleType, column.scale)} onChange={(value) => updateValueType(column.fieldId, value)} /></label>
        <div className={styles.columnState}><span className={styles.order}>{exportIndex >= 0 ? `第 ${exportIndex + 1} 列` : '不计入顺序'}</span><button type="button" disabled={!schema || !column.exportAllowed} aria-pressed={column.exportVisible && column.exportAllowed} onClick={() => toggleExport(column.fieldId, !column.exportVisible)}>{!column.exportAllowed ? '禁止导出' : column.exportVisible ? '已导出' : '不导出'}</button></div>
        <div className={styles.rowActions}><button type="button" disabled={exportIndex <= 0} aria-label={`上移 ${databaseColumn} 导出字段`} onClick={() => onChange(moveReportExportColumn(columns, column.fieldId, -1))}><ArrowUp aria-hidden="true" /></button><button type="button" disabled={exportIndex < 0 || exportIndex >= exportedColumns.length - 1} aria-label={`下移 ${databaseColumn} 导出字段`} onClick={() => onChange(moveReportExportColumn(columns, column.fieldId, 1))}><ArrowDown aria-hidden="true" /></button><button type="button" aria-label={`查看 ${databaseColumn} 字段详情`} onClick={() => setDetailColumn(column)}><Info aria-hidden="true" /></button><button className={styles.delete} type="button" disabled={!schema} aria-label={`删除 ${databaseColumn} Excel 映射`} onClick={() => onChange(columns.filter((item) => item.fieldId !== column.fieldId))}><Trash2 aria-hidden="true" /></button></div>
      </div> })}
      {orderedColumns.length === 0 && result.tableOwner && result.tableName && schemaState.status !== 'loading' ? <p className={styles.empty}>当前没有 Excel 映射，请从结果表字段中新增。</p> : null}
    </div>
    <ReportFieldDetailDrawer column={detailColumn} onClose={() => setDetailColumn(null)} />
  </div>
}

export type ExcelMappingSchemaState = { status: 'idle' | 'loading' | 'ready' | 'error'; error: string }
