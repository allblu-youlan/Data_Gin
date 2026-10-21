import { useState } from 'react'
import { Check, Plus, Search } from 'lucide-react'
import { Button, DataTable, FeedbackState, StatusTag } from '../../../ui'
import { isBojunColumnAllowed } from '../../bojunQueryConfig'
import type { BojunColumn, BojunTable } from '../../types'
import styles from './ReportQueryDesigner.module.css'

export function ReportTableFields({ table, columns, displayIds, detailIds, onDisplayChange, onDetailChange, onAppend }: { table: BojunTable; columns: BojunColumn[]; displayIds?: string[]; detailIds?: string[]; onDisplayChange?: (ids: string[]) => void; onDetailChange?: (ids: string[]) => void; onAppend: (columnId: string, label: string) => void }) {
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [added, setAdded] = useState<Record<string, number>>({})
  const toggle = (items: string[], id: string) => items.includes(id) ? items.filter((item) => item !== id) : [...items, id]
  const defaults = table.displayFields.length ? table.displayFields : table.primaryKeys
  const keyword = search.trim().toLocaleLowerCase()
  const shown = columns.filter((column) => `${column.dbName} ${column.name} ${column.description}`.toLocaleLowerCase().includes(keyword))
  const pageIndex = Math.min(page, Math.max(0, Math.ceil(shown.length / 15) - 1))
  const visible = shown.slice(pageIndex * 15, (pageIndex + 1) * 15)
  const usable = table.enabled && table.validationStatus === 'VALID'
  return <section className={styles.fieldPicker}>
    <div className={styles.header}><div><h3>字段与报表列</h3><p className={styles.hint}>搜索字段后加入报表；默认显示字段以徽标标记。</p></div><label className={styles.fieldSearch}><Search aria-hidden="true" /><input aria-label="搜索表字段" value={search} placeholder="字段名或业务名称" onChange={(event) => { setSearch(event.currentTarget.value); setPage(0) }} /></label></div>
    <DataTable density="compact" minWidth={onDisplayChange ? 640 : 420} scrollLabel="表字段与报表列"><thead><tr><th>字段</th><th>类型</th>{onDisplayChange ? <th>显示 / 详情</th> : null}<th>报表列</th></tr></thead><tbody>{visible.map((column) => {
      const id = String(column.id)
      const allowed = usable && isBojunColumnAllowed(table, column)
      const detailAllowed = usable && isBojunColumnAllowed(table, column, true)
      return <tr key={id}><td><div className={styles.fieldIdentity}><strong>{column.description || column.dbName}</strong><code>{column.dbName}</code><div className={styles.badges}>{defaults.includes(id) ? <StatusTag tone="info" showDot={false}>默认显示</StatusTag> : null}{table.primaryKeys.includes(id) ? <StatusTag showDot={false}>主键</StatusTag> : null}{!column.isPhysical ? <StatusTag tone="warning" showDot={false}>虚拟字段</StatusTag> : null}</div></div></td><td><code className={styles.hint}>{column.oracleType || '—'}</code></td>{onDisplayChange ? <td><div className={styles.checks}><label className={styles.check}><input type="checkbox" disabled={!allowed && !displayIds?.includes(id)} checked={displayIds?.includes(id) ?? false} onChange={() => onDisplayChange(toggle(displayIds ?? [], id))} />显示</label>{onDetailChange ? <label className={styles.check}><input type="checkbox" disabled={!detailAllowed && !detailIds?.includes(id)} checked={detailIds?.includes(id) ?? false} onChange={() => onDetailChange(toggle(detailIds ?? [], id))} />详情</label> : null}</div></td> : null}<td><Button disabled={!allowed} onClick={() => { onAppend(id, column.description || column.dbName); setAdded((current) => ({ ...current, [id]: (current[id] ?? 0) + 1 })) }}>{added[id] ? <Check aria-hidden="true" data-icon="inline-start" /> : <Plus aria-hidden="true" data-icon="inline-start" />}{added[id] ? `已加入 ${added[id]}` : '加入报表'}</Button></td></tr>
    })}</tbody></DataTable>
    {!shown.length ? <FeedbackState kind="empty" title="没有匹配的字段" description="试试字段编码或业务名称。" /> : null}
    {shown.length > 15 ? <div className={styles.header}><span className={styles.hint}>第 {pageIndex + 1} / {Math.ceil(shown.length / 15)} 页 · 共 {shown.length} 个字段</span><div className={styles.actions}><Button disabled={pageIndex === 0} onClick={() => setPage(pageIndex - 1)}>上一页字段</Button><Button disabled={(pageIndex + 1) * 15 >= shown.length} onClick={() => setPage(pageIndex + 1)}>下一页字段</Button></div></div> : null}
  </section>
}
