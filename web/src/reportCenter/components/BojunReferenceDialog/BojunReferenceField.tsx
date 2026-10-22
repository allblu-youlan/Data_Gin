import { ArrowUpRight } from 'lucide-react'
import type { BojunColumn } from '../../types'
import styles from './BojunReferenceDialog.module.css'

export function BojunReferenceField({ column, onInspect }: { column: BojunColumn; onInspect?: (column: BojunColumn, trigger: HTMLButtonElement) => void }) {
  const label = column.description || column.name || column.dbName
  return column.refTableId && onInspect ? <button type="button" className={styles.referenceLink} aria-label={`查看 ${label} 关联表详情`} title={`查看 ${column.dbName} 对应的关联表`} onClick={(event) => onInspect(column, event.currentTarget)}><span>{label}</span><ArrowUpRight aria-hidden="true" /></button> : <>{label || '—'}</>
}
