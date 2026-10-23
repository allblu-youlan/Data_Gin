import type { Dispatch, SetStateAction } from 'react'
import { DataTable } from '../ui'
import { oracleExportDefaultColumnTitle, type OracleExportColumnDraft } from './oracleExportConfig'
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

export { OracleExportFilters } from './OracleExportFilters'
