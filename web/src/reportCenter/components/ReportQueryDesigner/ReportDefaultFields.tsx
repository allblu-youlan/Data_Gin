import { StatusTag } from '../../../ui'
import type { BojunColumn, BojunTable } from '../../types'
import styles from './ReportQueryDesigner.module.css'

export function ReportDefaultFields({ table, columns, selected }: { table: BojunTable; columns: BojunColumn[]; selected?: string[] }) {
  const defaults = table.displayFields.length ? table.displayFields : table.primaryKeys
  const current = selected?.length ? selected : defaults
  return <div className={styles.defaultFields}>
    <span className={styles.metaLabel}>默认显示字段</span>
    <div className={styles.badges}>{defaults.map((id) => { const column = columns.find((item) => String(item.id) === id); return <StatusTag tone="info" showDot={false} key={id}>{column?.description || column?.dbName || `字段 #${id}`}<code>{column?.dbName ?? id}</code></StatusTag> })}</div>
    {selected && (current.length !== defaults.length || current.some((id, index) => id !== defaults[index])) ? <p className={styles.hint}>本次显示：{current.map((id) => columns.find((column) => String(column.id) === id)?.description || columns.find((column) => String(column.id) === id)?.dbName || id).join('、')}</p> : <p className={styles.hint}>关联记录以这些字段显示名称，原始关联键单独保留。</p>}
  </div>
}
