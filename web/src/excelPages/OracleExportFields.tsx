import type { Dispatch, SetStateAction } from 'react'
import { DataTable } from '../ui'
import { moveOracleExportColumn, oracleExportDefaultColumnTitle, type OracleExportColumnDraft } from './oracleExportConfig'
import styles from './OracleExportPage.module.css'

export function OracleExportFields({ columns, disabled, onChange }: { columns: OracleExportColumnDraft[]; disabled: boolean; onChange: Dispatch<SetStateAction<OracleExportColumnDraft[]>> }) {
  const selected = columns.filter((column) => column.selected)
  const selectedCount = selected.length
  const positions = new Map(selected.map((column, index) => [column.name, index + 1]))
  function update(name: string, patch: Partial<Pick<OracleExportColumnDraft, 'selected' | 'title'>>) {
    onChange((current) => current.map((column) => column.name === name ? { ...column, ...patch } : column))
  }
  return <div className={styles.stack}>
    <div className={styles.toolbar}><p>已选择 {selectedCount} / {columns.length} 个字段。通过“导出顺序”下拉框选择第几列，Excel 和已保存方案均使用此顺序。列名可手动修改。</p><div className={styles.actions}><button type="button" disabled={disabled || selectedCount === columns.length} onClick={() => onChange(columns.map((column) => ({ ...column, selected: true })))}>全选</button><button type="button" disabled={disabled || selectedCount === 0} onClick={() => onChange(columns.map((column) => ({ ...column, selected: false })))}>取消全选</button></div></div>
    <DataTable className={styles.fieldsTable} density="compact" minWidth={800} scrollLabel="Oracle 导出字段、列名与顺序">
      <thead><tr><th scope="col">导出</th><th scope="col">Oracle 字段</th><th scope="col">数据类型</th><th scope="col">Excel 列名</th><th scope="col">导出顺序</th></tr></thead>
      <tbody>{columns.map((column) => <tr key={column.name}>
        <td><input type="checkbox" aria-label={`导出字段 ${column.name}`} checked={column.selected} disabled={disabled} onChange={(event) => update(column.name, { selected: event.currentTarget.checked })} /></td>
        <td><code>{column.name}</code></td><td>{column.dataType}</td>
        <td><input className={styles.titleInput} aria-label={`${column.name} 的 Excel 列名`} value={column.title} placeholder={oracleExportDefaultColumnTitle(column)} maxLength={255} disabled={disabled || !column.selected} onChange={(event) => update(column.name, { title: event.currentTarget.value })} /></td>
        <td><select aria-label={`${column.name} 的导出顺序`} value={positions.get(column.name) ?? ''} disabled={disabled || !column.selected} onChange={(event) => { const position = Number(event.currentTarget.value); onChange((current) => moveOracleExportColumn(current, column.name, position)) }}>{column.selected ? selected.map((_, index) => <option value={index + 1} key={index + 1}>第 {index + 1} 列</option>) : <option value="">不导出</option>}</select></td>
      </tr>)}</tbody>
    </DataTable>
  </div>
}

export { OracleExportFilters } from './OracleExportFilters'
