import { useEffect, useMemo, useState } from 'react'
import { Search } from 'lucide-react'
import { FeedbackState, Section } from '../ui'
import { OracleExportFields, OracleExportFilters } from './OracleExportFields'
import { buildOracleExportRequest, parseOracleExportTables, type OracleExportSheetConfig, type OracleExportTable } from './oracleExportConfig'
import { useOracleExportSelection } from './useOracleExportSelection'
import type { ExcelPageClient } from './useExcelUploads'
import styles from './OracleExportPage.module.css'

export type OracleExportSheetSnapshot = { config: OracleExportSheetConfig | null; loading: boolean; issue: string }

export function OracleExportSheetEditor({ id, active, client, initial, sheetName, disabled, refreshVersion, onNameChange, onTableSelected, onSnapshot }: {
  id: number
  active: boolean
  client: ExcelPageClient
  initial?: OracleExportSheetConfig
  sheetName: string
  disabled: boolean
  refreshVersion: number
  onNameChange: (id: number, name: string) => void
  onTableSelected: (id: number, tableName: string) => void
  onSnapshot: (id: number, snapshot: OracleExportSheetSnapshot) => void
}) {
  const [owner, setOwner] = useState(initial?.owner ?? '')
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState({ owner: initial?.owner ?? '', search: '' })
  const [tables, setTables] = useState<OracleExportTable[]>([])
  const [tablesLoading, setTablesLoading] = useState(false)
  const [tablesError, setTablesError] = useState('')
  const selection = useOracleExportSelection(client)
  const { table, columns, filters, loading, error, issues, load } = selection

  useEffect(() => {
    if (initial) void load({ owner: initial.owner, name: initial.tableName, columnCount: initial.columns.length }, initial)
  }, [initial, load])

  useEffect(() => {
    if (!active) return
    const controller = new AbortController()
    setTablesLoading(true)
    setTablesError('')
    const params = new URLSearchParams({ owner: query.owner.trim(), search: query.search.trim(), limit: '100' })
    void (async () => {
      try {
        const response = await client(`/v1/excel-match-jobs/oracle-tables?${params}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true })
        if (controller.signal.aborted) return
        if (!response.ok) throw new Error(response.error?.message || 'Oracle 表目录加载失败，请重试。')
        setTables(parseOracleExportTables(response.data))
      } catch (cause) {
        if (!controller.signal.aborted) setTablesError(cause instanceof Error ? cause.message : 'Oracle 表目录加载失败，请重试。')
      } finally { if (!controller.signal.aborted) setTablesLoading(false) }
    })()
    return () => controller.abort()
  }, [active, client, query, refreshVersion])

  const snapshot = useMemo<OracleExportSheetSnapshot>(() => {
    if (loading) return { config: null, loading: true, issue: '正在读取字段…' }
    if (!table || error || issues.length) return { config: null, loading: false, issue: error || issues.join('；') || '请选择数据表' }
    try { return { config: { sheetName, ...buildOracleExportRequest(table, columns, filters) }, loading: false, issue: '' } }
    catch (cause) { return { config: null, loading: false, issue: cause instanceof Error ? cause.message : '请完成 Sheet 配置' } }
  }, [columns, error, filters, issues, loading, sheetName, table])

  useEffect(() => { onSnapshot(id, snapshot) }, [id, onSnapshot, snapshot])

  const tableOptions = table && !tables.some((item) => item.owner === table.owner && item.name === table.name) ? [table, ...tables] : tables
  function selectTable(value: string) {
    const next = tableOptions.find((item) => `${item.owner}.${item.name}` === value) ?? null
    void load(next)
    if (next) onTableSelected(id, next.name)
  }

  return <div className={styles.sheetEditor} hidden={!active} aria-label={`编辑 Sheet ${sheetName || id}`}>
    <Section title="选择数据表" description="每个 Sheet 对应一张表。Owner 留空时查询全部可访问 Owner，最多显示 100 张表。">
      <div className={styles.stack}>
        <label>Sheet 名称<input aria-label="Sheet 名称" value={sheetName} placeholder="选择数据表后默认使用表名" maxLength={31} disabled={disabled} onChange={(event) => onNameChange(id, event.currentTarget.value)} /><small className={styles.hint}>最多 31 个字符，名称不能重复，不能包含 []:*?/\ 或控制字符，首尾不能是单引号。</small></label>
        <form className={styles.catalog} onSubmit={(event) => { event.preventDefault(); setQuery({ owner, search }) }}>
          <label>Owner（可选）<input name="oracle_owner" value={owner} placeholder="全部可访问 Owner" disabled={disabled} onChange={(event) => setOwner(event.currentTarget.value)} /></label>
          <label>搜索表名<input name="oracle_table_search" type="search" value={search} placeholder="输入表名关键字" disabled={disabled} onChange={(event) => setSearch(event.currentTarget.value)} /></label>
          <button type="submit" disabled={tablesLoading || disabled}><Search aria-hidden="true" />{tablesLoading ? '查询中…' : '查询表'}</button>
        </form>
        {tablesError && <FeedbackState kind="error" title="表目录加载失败" description={tablesError} action={<button type="button" onClick={() => setQuery({ owner, search })} disabled={tablesLoading}>重试</button>} />}
        <label className={styles.tablePicker}>Oracle 数据表<select name="oracle_table" value={table ? `${table.owner}.${table.name}` : ''} disabled={tablesLoading || disabled} onChange={(event) => selectTable(event.currentTarget.value)}><option value="">{tablesLoading ? '正在加载表目录…' : '选择需要导出的表'}</option>{tableOptions.map((item) => <option key={`${item.owner}.${item.name}`} value={`${item.owner}.${item.name}`}>{item.owner}.{item.name} · {item.columnCount} 个字段</option>)}</select></label>
        {!tablesLoading && !tablesError && tables.length === 0 && <FeedbackState kind="empty" title="没有找到数据表" description="请调整 Owner 或表名关键字后重新查询。" />}
      </div>
    </Section>
    <Section title="导出字段与列名" description={table ? `${table.owner}.${table.name} · 此 Sheet 的字段、列名和筛选独立保存。` : '先选择数据表，再选择导出字段。'}>
      {loading ? <FeedbackState kind="loading" title="正在读取 Oracle 字段…" /> : error ? <FeedbackState kind="error" title="字段加载失败" description={error} action={<button type="button" onClick={() => void selection.retry()}>重试</button>} /> : columns.length ? <OracleExportFields columns={columns} disabled={disabled} onChange={selection.setColumns} /> : <FeedbackState kind="empty" title={table ? '该表没有可导出的字段' : '尚未选择数据表'} />}
      {issues.length > 0 && <FeedbackState kind="error" title="已保存配置包含失效字段" description={`${issues.join('；')}。当前已暂停保存和导出，请核对字段后明确移除失效引用。`} action={<button type="button" disabled={disabled} onClick={selection.removeInvalidReferences}>移除失效引用并继续配置</button>} />}
    </Section>
    <Section title="数据筛选（可选）"><OracleExportFilters columns={columns} filters={filters} disabled={disabled || loading || Boolean(error)} onChange={selection.setFilters} onAdd={selection.addFilter} /></Section>
  </div>
}
