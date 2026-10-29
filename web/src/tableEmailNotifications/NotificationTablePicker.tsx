import { useEffect, useState } from 'react'
import { Search } from 'lucide-react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { getNotificationTables } from './api'
import type { NotificationTable } from './types'
import shared from '../officeMessage/OfficeMessage.module.css'
import styles from './TableEmailNotifications.module.css'

type Props = { client: WorkspaceApiClient; datasourceId: number; owner: string; tableName: string; disabled: boolean; onSelect: (table: NotificationTable) => void }

export function NotificationTablePicker({ client, datasourceId, owner: selectedOwner, tableName, disabled, onSelect }: Props) {
  const [owner, setOwner] = useState(selectedOwner)
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState<{ owner: string; search: string; version: number } | null>(null)
  const [tables, setTables] = useState<NotificationTable[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!query) return
    const controller = new AbortController()
    setLoading(true)
    setError('')
    void getNotificationTables(client, datasourceId, query.owner, query.search, controller.signal).then((result) => {
      if (controller.signal.aborted) return
      if (result.ok) setTables(result.data)
      else { setTables([]); setError(result.error) }
      setLoading(false)
    })
    return () => controller.abort()
  }, [client, datasourceId, query])

  return <section className={shared.lookupPanel} aria-label="选择 Oracle 数据表">
    <h3>选择监听表</h3>
    <div className={styles.lookupFields}>
      <label>Schema / Owner<input value={owner} placeholder="留空查询可访问的 Schema" disabled={disabled} onChange={(event) => setOwner(event.target.value)} /></label>
      <label>表名搜索<input value={search} placeholder="输入表名关键字" disabled={disabled} onChange={(event) => setSearch(event.target.value)} /></label>
      <button type="button" disabled={disabled || loading} onClick={() => setQuery({ owner, search, version: (query?.version ?? 0) + 1 })}><Search aria-hidden="true" />{loading ? '查询中…' : '查询表'}</button>
    </div>
    {error ? <p className={shared.formError} role="alert">{error}</p> : tables.length > 0 ? <label>查询结果<select aria-label="选择监听表" value={tables.some((table) => table.owner === selectedOwner && table.name === tableName) ? `${selectedOwner}.${tableName}` : ''} disabled={disabled || loading} onChange={(event) => { const table = tables.find((item) => `${item.owner}.${item.name}` === event.target.value); if (table) onSelect(table) }}><option value="" disabled>选择查询到的数据表</option>{tables.map((table) => <option key={`${table.owner}.${table.name}`} value={`${table.owner}.${table.name}`}>{table.owner}.{table.name}（{table.columnCount} 个字段）</option>)}</select></label> : query && !loading ? <p className={shared.inlineEmpty}>未找到数据表，请调整 Schema 或表名后重试。</p> : <p className={shared.inlineEmpty}>查询当前数据源中可访问的 Oracle 表。</p>}
    <p className={shared.contractNote}>当前选择：<strong>{selectedOwner && tableName ? `${selectedOwner}.${tableName}` : '尚未选择'}</strong></p>
  </section>
}
