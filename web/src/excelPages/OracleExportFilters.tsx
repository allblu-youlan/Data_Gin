import { Plus, Trash2 } from 'lucide-react'
import type { Dispatch, SetStateAction } from 'react'
import { oracleExportFilterInputType, oracleExportFilterOperators, oracleExportFilterOperatorForType, type OracleExportColumnDraft, type OracleExportFilterDraft, type OracleExportFilterOperator } from './oracleExportConfig'
import { oracleExportComputedFilterError } from './oracleExportFilterSources'
import { OracleExportFilterValue } from './OracleExportFilterValue'
import styles from './OracleExportPage.module.css'

type FilterDefaults = Partial<Pick<OracleExportFilterDraft, 'op' | 'valueSource' | 'dateFormat'>>
export function OracleExportFilters({ columns, filters, disabled, onChange, onAdd }: { columns: OracleExportColumnDraft[]; filters: OracleExportFilterDraft[]; disabled: boolean; onChange: Dispatch<SetStateAction<OracleExportFilterDraft[]>>; onAdd: (defaults?: FilterDefaults) => void }) {
  const hasTimeColumn = columns.some((column) => oracleExportFilterInputType(column.dataType) !== 'text' || /^(?:N?VARCHAR2?|N?CHAR)(?:\b|\()/i.test(column.dataType.trim()))
  const hasMaximum = filters.some((filter) => filter.valueSource?.kind === 'COLUMN_MAX')
  return <div className={styles.stack}>
    <div className={styles.toolbar}><p>{filters.length ? '所有条件同时满足时导出（AND）。' : '未添加筛选条件，将导出所选表的全部数据。'}</p><button type="button" disabled={disabled || !columns.length} onClick={() => onAdd()}><Plus aria-hidden="true" />添加条件</button></div>
    <div className={styles.filterPresets} aria-label="动态筛选快捷入口"><span>快捷添加</span><button type="button" disabled={disabled || !hasTimeColumn} onClick={() => onAdd({ op: 'gte', dateFormat: 'yyyyMMdd', valueSource: { kind: 'TIME', code: 'MONTH_START' } })}>动态时间</button><button type="button" disabled={disabled || !hasTimeColumn} onClick={() => onAdd({ op: 'between', dateFormat: 'yyyyMMdd', valueSource: { kind: 'TIME_RANGE', code: 'CURRENT_MONTH' } })}>当月首日至末日</button><button type="button" disabled={disabled || !columns.length || hasMaximum} onClick={() => onAdd({ op: 'eq', valueSource: { kind: 'COLUMN_MAX' } })}>本列最大值</button></div>
    {filters.map((filter, index) => <FilterRow key={filter.id} filter={filter} index={index} columns={columns} disabled={disabled} otherMaximum={filters.some((item) => item.id !== filter.id && item.valueSource?.kind === 'COLUMN_MAX')} onChange={(next) => onChange((current) => current.map((item) => item.id === filter.id ? next : item))} onDelete={() => onChange((current) => current.filter((item) => item.id !== filter.id))} />)}
    <p className={styles.hint}>动态时间按任务创建时的北京时间计算，保存方案后下次导出会自动更新；本列最大值在其他筛选生效后计算。筛选字段可以不参与导出。</p>
  </div>
}

function FilterRow({ filter, index, columns, disabled, otherMaximum, onChange, onDelete }: { filter: OracleExportFilterDraft; index: number; columns: OracleExportColumnDraft[]; disabled: boolean; otherMaximum: boolean; onChange: (filter: OracleExportFilterDraft) => void; onDelete: () => void }) {
  const column = columns.find((column) => column.name === filter.field)
  const inputType = oracleExportFilterInputType(column?.dataType ?? '')
  const source = filter.valueSource
  function changeSource(kind: string) {
    const dateFormat = filter.dateFormat ?? 'yyyyMMdd'
    if (kind === 'TIME_RANGE') onChange({ ...filter, dateFormat, op: 'between', value: '', valueSource: { kind, code: 'CURRENT_MONTH' } })
    else if (kind === 'COLUMN_MAX') onChange({ ...filter, op: 'eq', value: '', valueSource: { kind } })
    else if (kind === 'TIME') onChange({ ...filter, dateFormat, op: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte'].includes(filter.op) ? filter.op : 'gte', value: '', valueSource: { kind, code: 'MONTH_START' } })
    else onChange({ ...filter, op: filter.op === 'between' ? 'eq' : filter.op, value: '', valueSource: undefined })
  }
  function changeField(field: string) {
    const dataType = columns.find((column) => column.name === field)?.dataType ?? ''
    const dateFormat = oracleExportFilterInputType(dataType) === 'number' ? filter.dateFormat ? 'yyyyMMdd' as const : undefined : filter.dateFormat ?? (oracleExportFilterInputType(dataType) === 'datetime-local' ? 'yyyyMMdd' as const : undefined)
    let next = { ...filter, field, dateFormat, value: '', op: oracleExportFilterOperatorForType(filter.op, dataType) }
    if (source && oracleExportComputedFilterError(next, dataType)) {
      next = { ...next, valueSource: source.kind === 'TIME' && oracleExportFilterInputType(dataType) !== 'text' ? { kind: 'TIME', code: 'MONTH_START' } : undefined }
      if (!next.valueSource && next.op === 'between') next.op = 'eq'
    }
    onChange(next)
  }
  const sourceLocked = source?.kind === 'TIME_RANGE' || source?.kind === 'COLUMN_MAX'
  return <div className={styles.filterRow}>
    <label>筛选字段 {index + 1}<select aria-label={`筛选字段 ${index + 1}`} value={filter.field} disabled={disabled} required onChange={(event) => changeField(event.currentTarget.value)}><option value="">选择字段</option>{filter.field && !column ? <option value={filter.field} disabled>{filter.field} · 字段已失效</option> : null}{columns.map((item) => <option key={item.name} value={item.name}>{item.name} · {item.dataType}</option>)}</select></label>
    <label>比较方式<select aria-label={`条件 ${index + 1} 比较方式`} value={filter.op} disabled={disabled || sourceLocked} onChange={(event) => { const op = event.currentTarget.value as OracleExportFilterOperator; onChange({ ...filter, op, valueSource: op === 'empty' || op === 'not_empty' ? undefined : source }) }}>{oracleExportFilterOperators.filter((operator) => (operator.value !== 'contains' || inputType === 'text') && (operator.value !== 'between' || source?.kind === 'TIME_RANGE')).map((operator) => <option key={operator.value} value={operator.value}>{operator.label}</option>)}</select></label>
    <label>值来源<select aria-label={`条件 ${index + 1} 值来源`} value={source?.kind ?? 'CONSTANT'} disabled={disabled} onChange={(event) => changeSource(event.currentTarget.value)}><option value="CONSTANT">手动填写</option>{inputType !== 'text' || /^(?:N?VARCHAR2?|N?CHAR)(?:\b|\()/i.test(column?.dataType ?? '') || !filter.field || source?.kind === 'TIME' || source?.kind === 'TIME_RANGE' ? <><option value="TIME">动态时间</option><option value="TIME_RANGE">动态月份范围</option></> : null}<option value="COLUMN_MAX" disabled={otherMaximum}>本列最大值</option></select></label>
    <OracleExportFilterValue filter={filter} index={index} dataType={column?.dataType ?? ''} disabled={disabled} onChange={onChange} />
    <button type="button" disabled={disabled} aria-label={`删除筛选条件 ${index + 1}`} onClick={onDelete}><Trash2 aria-hidden="true" />删除</button>
  </div>
}
