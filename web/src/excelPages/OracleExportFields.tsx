import { Plus, Trash2 } from 'lucide-react'
import { DataTable } from '../ui'
import { oracleExportFilterNeedsValue, oracleExportFilterOperators, type OracleExportColumnDraft, type OracleExportFilterDraft, type OracleExportFilterOperator } from './oracleExportConfig'
import styles from './OracleExportPage.module.css'

export function OracleExportFields({ columns, disabled, onChange }: { columns: OracleExportColumnDraft[]; disabled: boolean; onChange: (columns: OracleExportColumnDraft[]) => void }) {
  const selectedCount = columns.filter((column) => column.selected).length
  function update(name: string, patch: Partial<Pick<OracleExportColumnDraft, 'selected' | 'title'>>) {
    onChange(columns.map((column) => column.name === name ? { ...column, ...patch } : column))
  }
  return <div className={styles.stack}>
    <div className={styles.toolbar}><p>已选择 {selectedCount} / {columns.length} 个字段，按表字段顺序导出。</p><div className={styles.actions}><button type="button" disabled={disabled || selectedCount === columns.length} onClick={() => onChange(columns.map((column) => ({ ...column, selected: true })))}>全选</button><button type="button" disabled={disabled || selectedCount === 0} onClick={() => onChange(columns.map((column) => ({ ...column, selected: false })))}>取消全选</button></div></div>
    <DataTable density="compact" minWidth={620} scrollLabel="Oracle 导出字段与 Excel 列名">
      <thead><tr><th scope="col">导出</th><th scope="col">Oracle 字段</th><th scope="col">数据类型</th><th scope="col">Excel 列名</th></tr></thead>
      <tbody>{columns.map((column) => <tr key={column.name}>
        <td><input type="checkbox" aria-label={`导出字段 ${column.name}`} checked={column.selected} disabled={disabled} onChange={(event) => update(column.name, { selected: event.currentTarget.checked })} /></td>
        <td><code>{column.name}</code></td><td>{column.dataType}</td>
        <td><input className={styles.titleInput} aria-label={`${column.name} 的 Excel 列名`} value={column.title} placeholder={column.name} maxLength={255} disabled={disabled || !column.selected} onChange={(event) => update(column.name, { title: event.currentTarget.value })} /></td>
      </tr>)}</tbody>
    </DataTable>
  </div>
}

export function OracleExportFilters({ columns, filters, disabled, onChange, onAdd }: { columns: OracleExportColumnDraft[]; filters: OracleExportFilterDraft[]; disabled: boolean; onChange: (filters: OracleExportFilterDraft[]) => void; onAdd: () => void }) {
  function update(id: number, patch: Partial<OracleExportFilterDraft>) {
    onChange(filters.map((filter) => filter.id === id ? { ...filter, ...patch } : filter))
  }
  return <div className={styles.stack}>
    <div className={styles.toolbar}><p>{filters.length ? '所有条件同时满足时导出（AND）。' : '未添加筛选条件，将导出所选表的全部数据。'}</p><button type="button" disabled={disabled || !columns.length} onClick={onAdd}><Plus aria-hidden="true" />添加条件</button></div>
    {filters.map((filter, index) => <div className={styles.filterRow} key={filter.id}>
      <label>筛选字段 {index + 1}<select aria-label={`筛选字段 ${index + 1}`} value={filter.field} disabled={disabled} required onChange={(event) => update(filter.id, { field: event.currentTarget.value })}><option value="">选择字段</option>{columns.map((column) => <option key={column.name} value={column.name}>{column.name} · {column.dataType}</option>)}</select></label>
      <label>比较方式<select aria-label={`条件 ${index + 1} 比较方式`} value={filter.op} disabled={disabled} onChange={(event) => update(filter.id, { op: event.currentTarget.value as OracleExportFilterOperator })}>{oracleExportFilterOperators.map((operator) => <option key={operator.value} value={operator.value}>{operator.label}</option>)}</select></label>
      <label>筛选值<input aria-label={`条件 ${index + 1} 筛选值`} value={oracleExportFilterNeedsValue(filter.op) ? filter.value : ''} disabled={disabled || !oracleExportFilterNeedsValue(filter.op)} required={oracleExportFilterNeedsValue(filter.op)} placeholder={oracleExportFilterNeedsValue(filter.op) ? '输入筛选值' : '无需填写'} onChange={(event) => update(filter.id, { value: event.currentTarget.value })} /></label>
      <button type="button" disabled={disabled} aria-label={`删除筛选条件 ${index + 1}`} onClick={() => onChange(filters.filter((item) => item.id !== filter.id))}><Trash2 aria-hidden="true" />删除</button>
    </div>)}
    {filters.length > 0 && <p className={styles.hint}>日期格式：YYYY-MM-DD 或 YYYY-MM-DD HH:mm:ss。筛选字段可以不参与导出。</p>}
  </div>
}
