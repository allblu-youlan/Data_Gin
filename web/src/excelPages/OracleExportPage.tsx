import { type FormEvent, useEffect, useRef, useState } from 'react'
import { Download, Search } from 'lucide-react'
import type { ClientResponse } from '../api/client'
import { FeedbackState, PageCanvas, PageHeader, Section } from '../ui'
import { ExcelJobDetailContent } from './ExcelMatchPageParts'
import { excelJobProgressPercent } from './excelPageSupport'
import { OracleExportFields, OracleExportFilters } from './OracleExportFields'
import { buildOracleExportRequest, parseOracleExportTables, type OracleExportTable } from './oracleExportConfig'
import { OracleExportSchemeBar } from './OracleExportSchemeBar'
import { useOracleExportSelection } from './useOracleExportSelection'
import { useExcelJobs } from './useExcelJobs'
import type { ExcelPageClient } from './useExcelUploads'
import styles from './OracleExportPage.module.css'

export function OracleExportPage({ client, token, loading, refreshVersion, canExecute, canManage, setLoading, setResult, onNavigateToJobs }: {
  client: ExcelPageClient
  token: string
  loading: boolean
  refreshVersion: number
  canExecute: boolean
  canManage: boolean
  setLoading: (value: boolean) => void
  setResult: (value: ClientResponse | null) => void
  onNavigateToJobs: () => void
}) {
  const jobs = useExcelJobs({ section: 'schemes', client, token, refreshVersion, setLoading, setResult })
  const [owner, setOwner] = useState('')
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState({ owner: '', search: '' })
  const [tables, setTables] = useState<OracleExportTable[]>([])
  const [tablesLoading, setTablesLoading] = useState(true)
  const [tablesError, setTablesError] = useState('')
  const selection = useOracleExportSelection(client)
  const { table, columns, filters } = selection
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const submitInFlight = useRef(false)

  useEffect(() => {
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
      } catch (error) {
        if (!controller.signal.aborted) setTablesError(error instanceof Error ? error.message : 'Oracle 表目录加载失败，请重试。')
      } finally {
        if (!controller.signal.aborted) setTablesLoading(false)
      }
    })()
    return () => controller.abort()
  }, [client, query, refreshVersion])

  function selectTable(value: string) {
    void selection.load(tableOptions.find((item) => `${item.owner}.${item.name}` === value) ?? null)
    setSubmitError('')
  }

  function currentConfig() {
    if (!table || selection.loading || selection.error || selection.issues.length) throw new Error('请先选择数据表并处理字段加载提示。')
    return buildOracleExportRequest(table, columns, filters)
  }

  async function createExport(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!table || !canExecute || submitInFlight.current) return
    setSubmitError('')
    submitInFlight.current = true
    setSubmitting(true)
    try {
      const body = currentConfig()
      const response = await client('/v1/excel-match-jobs/oracle-exports', { method: 'POST', body, showResult: false, silentLoading: true, retry: false })
      if (!response.ok) throw new Error(response.error?.message || 'Oracle 导出任务创建失败，请重试。')
      if (!jobs.registerCreatedJob(response, false)) throw new Error('任务响应缺少任务信息，请到 Excel 任务中查询。')
    } catch (error) {
      setSubmitError(error instanceof Error ? error.message : 'Oracle 导出任务创建失败，请重试。')
    } finally {
      submitInFlight.current = false
      setSubmitting(false)
    }
  }

  const tableOptions = table && !tables.some((item) => item.owner === table.owner && item.name === table.name) ? [table, ...tables] : tables
  const busy = submitting || loading
  const configReady = Boolean(table) && !selection.loading && !selection.error && !selection.issues.length && columns.some((column) => column.selected)
  return <PageCanvas className={styles.page} density="compact">
    <PageHeader eyebrow="EXCEL WORKSPACE" title="Excel 导出" description="从默认 Oracle 环境选择数据表，按字段和条件生成 Excel。" actions={<button type="button" onClick={onNavigateToJobs}>查看 Excel 任务</button>} />
    <OracleExportSchemeBar client={client} canManage={canManage} disabled={busy || selection.loading} canSave={configReady} refreshVersion={refreshVersion} getConfig={currentConfig} onLoad={(scheme) => { setSubmitError(''); setOwner(scheme.config.owner); return selection.load({ owner: scheme.config.owner, name: scheme.config.tableName, columnCount: scheme.config.columns.length }, scheme.config) }} />
    <Section title="选择数据表" description="使用系统配置的 Oracle 连接。Owner 留空时查询全部可访问 Owner；最多显示 100 张表，可按表名缩小范围。">
      <div className={styles.stack}>
        <form className={styles.catalog} onSubmit={(event) => { event.preventDefault(); setQuery({ owner, search }) }}>
          <label>Owner（可选）<input name="oracle_owner" value={owner} placeholder="全部可访问 Owner" disabled={busy} onChange={(event) => setOwner(event.currentTarget.value)} /></label>
          <label>搜索表名<input name="oracle_table_search" type="search" value={search} placeholder="输入表名关键字" disabled={busy} onChange={(event) => setSearch(event.currentTarget.value)} /></label>
          <button type="submit" disabled={tablesLoading || busy}><Search aria-hidden="true" />{tablesLoading ? '查询中…' : '查询表'}</button>
        </form>
        {tablesError && <FeedbackState kind="error" title="表目录加载失败" description={tablesError} action={<button type="button" onClick={() => setQuery({ owner, search })} disabled={tablesLoading}>重试</button>} />}
        <label className={styles.tablePicker}>Oracle 数据表<select name="oracle_table" value={table ? `${table.owner}.${table.name}` : ''} disabled={tablesLoading || busy} onChange={(event) => selectTable(event.currentTarget.value)}><option value="">{tablesLoading ? '正在加载表目录…' : '选择需要导出的表'}</option>{tableOptions.map((item) => <option key={`${item.owner}.${item.name}`} value={`${item.owner}.${item.name}`}>{item.owner}.{item.name} · {item.columnCount} 个字段</option>)}</select></label>
        {!tablesLoading && !tablesError && tables.length === 0 && <FeedbackState kind="empty" title="没有找到数据表" description="请调整 Owner 或表名关键字后重新查询。" />}
      </div>
    </Section>
    <form className={styles.stack} onSubmit={(event) => void createExport(event)}>
      <Section title="导出字段与列名" description={table ? `${table.owner}.${table.name} · 默认导出全部字段，Excel 列名可直接修改。` : '先选择数据表，再选择导出字段。'}>
        {selection.loading ? <FeedbackState kind="loading" title="正在读取 Oracle 字段…" /> : selection.error ? <FeedbackState kind="error" title="字段加载失败" description={selection.error} action={<button type="button" onClick={() => void selection.retry()}>重试</button>} /> : columns.length ? <OracleExportFields columns={columns} disabled={busy} onChange={selection.setColumns} /> : <FeedbackState kind="empty" title={table ? '该表没有可导出的字段' : '尚未选择数据表'} />}
        {selection.issues.length > 0 && <FeedbackState kind="error" title="已保存配置包含失效字段" description={`${selection.issues.join('；')}。当前已暂停保存和导出，请核对字段后明确移除失效引用。`} action={<button type="button" disabled={busy} onClick={selection.removeInvalidReferences}>移除失效引用并继续配置</button>} />}
      </Section>
      <Section title="数据筛选（可选）"><OracleExportFilters columns={columns} filters={filters} disabled={busy || selection.loading || Boolean(selection.error)} onChange={selection.setFilters} onAdd={selection.addFilter} /></Section>
      {submitError && <FeedbackState kind="error" title="导出任务未创建" description={submitError} />}
      <div className={styles.submit}><p>每份 Excel 最多 100 万行；超出后分成多个 Excel，以 ZIP 下载。结果保留 24 小时。</p><button type="submit" disabled={!canExecute || busy || !configReady}><Download aria-hidden="true" />{submitting ? '正在创建…' : '创建导出任务'}</button></div>
      {!canExecute && <p className={styles.hint}>当前账号没有执行 Excel 任务的权限。</p>}
    </form>
    {jobs.job && <Section title={`最近导出任务 #${jobs.job.id}`} description="可留在此页跟踪进度，或到 Excel 任务查看历史。"><ExcelJobDetailContent job={jobs.job} logs={jobs.jobLogs} progress={excelJobProgressPercent(jobs.job)} autoRefreshText={jobs.autoRefreshText} loading={loading} downloading={jobs.downloadingJobID === jobs.job.id} onRefresh={() => void jobs.refreshJobByID(jobs.job!.id)} onDownload={() => void jobs.downloadJob(jobs.job!.id)} /></Section>}
  </PageCanvas>
}
