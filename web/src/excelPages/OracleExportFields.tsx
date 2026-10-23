import { Plus, Trash2 } from 'lucide-react'
import type { Dispatch, SetStateAction } from 'react'
import { DataTable } from '../ui'
import { oracleExportDateInputValue, oracleExportDefaultColumnTitle, oracleExportFilterInputType, oracleExportFilterNeedsValue, oracleExportFilterOperators, oracleExportFilterOperatorForType, type OracleExportColumnDraft, type OracleExportFilterDraft, type OracleExportFilterOperator } from './oracleExportConfig'
import styles from './OracleExportPage.module.css'

export function OracleExportFields({ columns, disabled, onChange }: { columns: OracleExportColumnDraft[]; disabled: boolean; onChange: Dispatch<SetStateAction<OracleExportColumnDraft[]>> }) {
  const selectedCount = columns.filter((column) => column.selected).length
  function update(name: string, patch: Partial<Pick<OracleExportColumnDraft, 'selected' | 'title'>>) {
    onChange((current) => current.map((column) => column.name === name ? { ...column, ...patch } : column))
  }
  return <div className={styles.stack}>
    <div className={styles.toolbar}><p>已选择 {selectedCount} / {columns.length} 个字段，按表字段顺序导出。列名默认使用字段注释，无注释时使用字段名，可手动修改。</p><div className={styles.actions}><button type="button" disabled={disabled || selectedCount === columns.length} onClick={() => onChange(columns.map((column) => ({ ...column, selected: true })))}>全选</button><button type="button" disabled={disabled || selectedCount === 0} onClick={() => onChange(columns.map((column) => ({ ...column, selected: false })))}>取消全选</button></div></div>
    <DataTable className={styles.fieldsTable} density="compact" minWidth={620} scrollLabel="Oracle 导出字段与 Excel 列名">
      <thead><tr><th scope="col">导出</th><th scope="col">Oracle 字段</th><th scope="col">数据类型</th><th scope="col">Excel 列名</th></tr></thead>
      <tbody>{columns.map((column) => <tr key={column.name}>
        <td><input type="checkbox" aria-label={`导出字段 ${column.name}`} checked={column.selected} disabled={disabled} onChange={(event) => update(column.name, { selected: event.currentTarget.checked })} /></td>
        <td><code>{column.name}</code></td><td>{column.dataType}</td>
        <td><input className={styles.titleInput} aria-label={`${column.name} 的 Excel 列名`} value={column.title} placeholder={oracleExportDefaultColumnTitle(column)} maxLength={255} disabled={disabled || !column.selected} onChange={(event) => update(column.name, { title: event.currentTarget.value })} /></td>
      </tr>)}</tbody>
    </DataTable>
  </div>
}

export function OracleExportFilters({ columns, filters, disabled, onChange, onAdd }: { columns: OracleExportColumnDraft[]; filters: OracleExportFilterDraft[]; disabled: boolean; onChange: Dispatch<SetStateAction<OracleExportFilterDraft[]>>; onAdd: () => void }) {
  function update(id: number, patch: Partial<OracleExportFilterDraft>) {
    onChange((current) => current.map((filter) => filter.id === id ? { ...filter, ...patch } : filter))
  }
  return <div className={styles.stack}>
    <div className={styles.toolbar}><p>{filters.length ? '所有条件同时满足时导出（AND）。' : '未添加筛选条件，将导出所选表的全部数据。'}</p><button type="button" disabled={disabled || !columns.length} onClick={onAdd}><Plus aria-hidden="true" />添加条件</button></div>
    {filters.map((filter, index) => {
      const column = columns.find((column) => column.name === filter.field)
      const inputType = oracleExportFilterInputType(column?.dataType ?? '')
      const needsValue = oracleExportFilterNeedsValue(filter.op)
      return <div className={styles.filterRow} key={filter.id}>
      <label>筛选字段 {index + 1}<select aria-label={`筛选字段 ${index + 1}`} value={filter.field} disabled={disabled} required onChange={(event) => { const field = event.currentTarget.value; update(filter.id, { field, value: '', op: oracleExportFilterOperatorForType(filter.op, columns.find((column) => column.name === field)?.dataType ?? '') }) }}><option value="">选择字段</option>{filter.field && !column && <option value={filter.field} disabled>{filter.field} · 字段已失效</option>}{columns.map((column) => <option key={column.name} value={column.name}>{column.name} · {column.dataType}</option>)}</select></label>
      <label>比较方式<select aria-label={`条件 ${index + 1} 比较方式`} value={filter.op} disabled={disabled} onChange={(event) => update(filter.id, { op: event.currentTarget.value as OracleExportFilterOperator })}>{oracleExportFilterOperators.filter((operator) => operator.value !== 'contains' || inputType === 'text').map((operator) => <option key={operator.value} value={operator.value}>{operator.label}</option>)}</select></label>
      <label>{inputType === 'datetime-local' ? '筛选时间' : '筛选值'}<input aria-label={`条件 ${index + 1} ${inputType === 'datetime-local' ? '筛选时间' : '筛选值'}`} type={inputType} step={inputType === 'datetime-local' ? 1 : inputType === 'number' ? 'any' : undefined} value={needsValue ? inputType === 'datetime-local' ? oracleExportDateInputValue(filter.value) : filter.value : ''} disabled={disabled || !needsValue} required={needsValue} placeholder={needsValue ? '输入筛选值' : '无需填写'} onInput={inputType === 'datetime-local' ? (event) => update(filter.id, { value: event.currentTarget.value }) : undefined} onChange={(event) => update(filter.id, { value: event.currentTarget.value })} />{inputType === 'datetime-local' && <small className={styles.hint}>YYYY-MM-DD HH:mm:ss，精确到秒。</small>}</label>
      <button type="button" disabled={disabled} aria-label={`删除筛选条件 ${index + 1}`} onClick={() => onChange((current) => current.filter((item) => item.id !== filter.id))}><Trash2 aria-hidden="true" />删除</button>
    </div>})}
    {filters.length > 0 && <p className={styles.hint}>日期时间按数据库时间填写；可用“大于等于”和“小于等于”设置起止时间。筛选字段可以不参与导出。</p>}
  </div>
}
