import { useState } from 'react'
import { Search } from 'lucide-react'
import { Button } from '../../../ui'
import { isBojunColumnAllowed } from '../../bojunQueryConfig'
import type { BojunColumn, BojunTable } from '../../types'
import styles from './ReportTablesWorkspace.module.css'

export function ReportWorkspaceFields({ table, columns, selectedCounts, onToggle }: { table: BojunTable; columns: BojunColumn[]; selectedCounts: Record<string, number>; onToggle: (id: string, header: string, checked: boolean) => void }) {
  const [search, setSearch] = useState('')
  const [selectedOnly, setSelectedOnly] = useState(false)
  const [page, setPage] = useState(0)
  const defaults = table.displayFields.length ? table.displayFields : table.primaryKeys
  const shown = columns.filter((column) => (!selectedOnly || selectedCounts[String(column.id)]) && `${column.dbName} ${column.description} ${column.name}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  const pageSize = 30
  const pageIndex = Math.min(page, Math.max(0, Math.ceil(shown.length / pageSize) - 1))
  const usable = table.enabled && table.validationStatus === 'VALID'
  return <div className={styles.fields}>
    <div className={styles.fieldTools}><label className={styles.search}><Search aria-hidden="true" /><input name="report-field-search" autoComplete="off" aria-label="搜索表字段" placeholder="搜索字段名称或编码…" value={search} onChange={(event) => { setSearch(event.currentTarget.value); setPage(0) }} /></label><label className={styles.selectedOnly}><input type="checkbox" checked={selectedOnly} onChange={(event) => { setSelectedOnly(event.currentTarget.checked); setPage(0) }} />仅看已选</label><span>{shown.length} 个字段</span></div>
    <div className={styles.fieldScroll} tabIndex={0} role="region" aria-label="当前表字段清单，可滚动查看"><table className={styles.fieldTable}><thead><tr><th>选择</th><th>字段编码</th><th>业务名称</th><th>类型</th><th>字典定义</th></tr></thead><tbody>{shown.slice(pageIndex * pageSize, (pageIndex + 1) * pageSize).map((column) => {
      const id = String(column.id)
      const allowed = usable && isBojunColumnAllowed(table, column)
      const count = selectedCounts[id] ?? 0
      return <tr key={id} data-selected={count > 0}><td><input type="checkbox" aria-label={`输出 ${column.description || column.dbName}`} disabled={!allowed && !count} checked={count > 0} onChange={(event) => onToggle(id, column.description || column.dbName, event.currentTarget.checked)} /></td><td><code title={column.dbName}>{column.dbName}</code></td><td>{column.description || column.name || '—'}</td><td><code className={styles.type}>{column.oracleType || column.logicalType || '—'}</code></td><td><div className={styles.badges}>{defaults.includes(id) ? <span className={styles.defaultBadge}>默认显示</span> : null}{table.primaryKeys.includes(id) ? <span className={styles.keyBadge}>主键</span> : null}{column.refTableId ? <span className={styles.referenceBadge}>关联</span> : null}{!column.isPhysical ? <span className={styles.keyBadge}>虚拟</span> : null}{!allowed ? <span className={styles.keyBadge}>不可用</span> : null}</div></td></tr>
    })}</tbody></table>{!shown.length ? <p className={styles.emptyHint}>{selectedOnly ? '当前表没有匹配的已选字段' : '没有匹配的字段，请调整关键词'}</p> : null}</div>
    <footer className={styles.fieldsFooter}><span>{Object.keys(selectedCounts).length} 个已选字段 · 默认显示沿用伯俊字典</span>{shown.length > pageSize ? <div><span>{pageIndex + 1} / {Math.ceil(shown.length / pageSize)}</span><Button disabled={pageIndex === 0} onClick={() => setPage(pageIndex - 1)}>上一页</Button><Button disabled={(pageIndex + 1) * pageSize >= shown.length} onClick={() => setPage(pageIndex + 1)}>下一页</Button></div> : null}</footer>
  </div>
}
