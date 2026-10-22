import { useCallback, useRef, useState } from 'react'
import { Download, Plus, Trash2 } from 'lucide-react'
import type { ClientResponse } from '../api/client'
import { FeedbackState, PageCanvas, PageHeader, Section } from '../ui'
import { ExcelJobDetailContent } from './ExcelMatchPageParts'
import { excelJobProgressPercent } from './excelPageSupport'
import { buildOracleExportWorkbookRequest, defaultOracleExportSheetName, oracleExportSheetLimit, type OracleExportSheetConfig } from './oracleExportConfig'
import { OracleExportSchemeBar } from './OracleExportSchemeBar'
import { OracleExportSheetEditor, type OracleExportSheetSnapshot } from './OracleExportSheetEditor'
import { useExcelJobs } from './useExcelJobs'
import type { ExcelPageClient } from './useExcelUploads'
import styles from './OracleExportPage.module.css'

type SheetDraft = { id: number; sheetName: string; defaultName: boolean; initial?: OracleExportSheetConfig }

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
  const nextID = useRef(1)
  const [sheets, setSheets] = useState<SheetDraft[]>([{ id: 1, sheetName: '', defaultName: true }])
  const [activeID, setActiveID] = useState(1)
  const [snapshots, setSnapshots] = useState<Record<number, OracleExportSheetSnapshot>>({})
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const submitInFlight = useRef(false)
  const onSnapshot = useCallback((id: number, snapshot: OracleExportSheetSnapshot) => setSnapshots((current) => ({ ...current, [id]: snapshot })), [])
  const onNameChange = useCallback((id: number, sheetName: string) => setSheets((current) => current.map((sheet) => sheet.id === id ? { ...sheet, sheetName, defaultName: false } : sheet)), [])
  const onTableSelected = useCallback((id: number, tableName: string) => setSheets((current) => current.map((sheet) => sheet.id === id && sheet.defaultName ? { ...sheet, sheetName: defaultOracleExportSheetName(tableName, current.filter((item) => item.id !== id).map((item) => item.sheetName)) } : sheet)), [])

  function currentConfig() {
    return buildOracleExportWorkbookRequest(sheets.map((sheet, index) => {
      const snapshot = snapshots[sheet.id]
      if (!snapshot?.config) throw new Error(`第 ${index + 1} 个 Sheet（${sheet.sheetName || '未命名'}）：${snapshot?.issue || '请先选择数据表'}。`)
      return { ...snapshot.config, sheetName: sheet.sheetName }
    }))
  }

  function addSheet() {
    if (sheets.length >= oracleExportSheetLimit) return
    const id = ++nextID.current
    setSheets((current) => [...current, { id, sheetName: '', defaultName: true }])
    setActiveID(id)
    setSubmitError('')
  }

  function removeSheet() {
    if (sheets.length <= 1) return
    const remaining = sheets.filter((sheet) => sheet.id !== activeID)
    setSheets(remaining)
    setActiveID(remaining[0].id)
    setSubmitError('')
  }

  async function createExport() {
    if (!canExecute || submitInFlight.current) return
    setSubmitError('')
    submitInFlight.current = true
    setSubmitting(true)
    try {
      const body = currentConfig()
      const response = await client('/v1/excel-match-jobs/oracle-exports', { method: 'POST', body, showResult: false, silentLoading: true, retry: false })
      if (!response.ok) throw new Error(response.error?.message || 'Oracle 导出任务创建失败，请重试。')
      if (!jobs.registerCreatedJob(response, false)) throw new Error('任务响应缺少任务信息，请到 Excel 任务中查询。')
    } catch (cause) { setSubmitError(cause instanceof Error ? cause.message : 'Oracle 导出任务创建失败，请重试。') }
    finally { submitInFlight.current = false; setSubmitting(false) }
  }

  let configIssue = ''
  try { currentConfig() } catch (cause) { configIssue = cause instanceof Error ? cause.message : '请完成全部 Sheet 配置。' }
  const busy = submitting || loading
  const reading = sheets.some((sheet) => snapshots[sheet.id]?.loading)
  return <PageCanvas className={styles.page} density="compact">
    <PageHeader eyebrow="EXCEL WORKSPACE" title="Excel 导出" description="从默认 Oracle 环境选择数据表，一个配置可导出多个 Sheet，各 Sheet 独立设置字段和筛选。" actions={<button type="button" onClick={onNavigateToJobs}>查看 Excel 任务</button>} />
    <OracleExportSchemeBar client={client} canManage={canManage} disabled={busy || reading} canSave={!configIssue} refreshVersion={refreshVersion} getConfig={currentConfig} onLoad={async (scheme) => {
      const restored = scheme.config.sheets.map((initial) => ({ id: ++nextID.current, sheetName: initial.sheetName, defaultName: false, initial }))
      setSnapshots({})
      setSheets(restored)
      setActiveID(restored[0].id)
      setSubmitError('')
      return true
    }} />
    <Section title={`工作表（${sheets.length} / ${oracleExportSheetLimit}）`} description="切换 Sheet 会保留当前编辑内容。每个 Sheet 对应一张 Oracle 表。">
      <div className={styles.stack}>
        <div className={styles.sheetBar}><label>当前编辑的 Sheet<select aria-label="当前编辑的 Sheet" value={activeID} disabled={busy} onChange={(event) => setActiveID(Number(event.currentTarget.value))}>{sheets.map((sheet, index) => <option key={sheet.id} value={sheet.id}>{index + 1}. {sheet.sheetName || '未命名 Sheet'}{snapshots[sheet.id]?.issue ? ` · ${snapshots[sheet.id].loading ? '读取中' : '待配置'}` : ''}</option>)}</select></label><div className={styles.actions}><button type="button" disabled={busy || sheets.length >= oracleExportSheetLimit} onClick={addSheet}><Plus aria-hidden="true" />添加 Sheet</button><button type="button" disabled={busy || sheets.length <= 1} onClick={removeSheet}><Trash2 aria-hidden="true" />移除当前 Sheet</button></div></div>
        {configIssue && <p className={styles.hint} role="status">{configIssue}</p>}
      </div>
    </Section>
    {sheets.map((sheet) => <OracleExportSheetEditor key={sheet.id} id={sheet.id} active={activeID === sheet.id} client={client} initial={sheet.initial} sheetName={sheet.sheetName} disabled={busy} refreshVersion={refreshVersion} onNameChange={onNameChange} onTableSelected={onTableSelected} onSnapshot={onSnapshot} />)}
    <Section title="多文件拆分规则" description="每个 Sheet 每份文件最多 100 万行；有多份时以 ZIP 下载，结果保留 24 小时。">
      <div className={styles.stack}><p className={styles.hint}>超过 100 万行的表按相同片序号写入各份 Excel；不超过 100 万行的表会在每份 Excel 中完整复制。大表后续无数据时，该 Sheet 保留表头。总文件数取各表分片数的最大值。</p><p className={styles.hint}>例如：A 表 150 万行、B 表 20 万行，将生成 2 份 Excel。A Sheet 分别为 100 万行和 50 万行，B Sheet 两份均为完整 20 万行。</p></div>
    </Section>
    {submitError && <FeedbackState kind="error" title="导出任务未创建" description={submitError} />}
    <div className={styles.submit}><p>共 {sheets.length} 个 Sheet。{configIssue || '全部 Sheet 已就绪，可保存配置或创建导出任务。'}</p><button type="button" disabled={!canExecute || busy || Boolean(configIssue)} onClick={() => void createExport()}><Download aria-hidden="true" />{submitting ? '正在创建…' : '创建导出任务'}</button></div>
    {!canExecute && <p className={styles.hint}>当前账号没有执行 Excel 任务的权限。</p>}
    {jobs.job && <Section title={`最近导出任务 #${jobs.job.id}`} description="可留在此页跟踪进度，或到 Excel 任务查看历史。"><ExcelJobDetailContent job={jobs.job} logs={jobs.jobLogs} progress={excelJobProgressPercent(jobs.job)} autoRefreshText={jobs.autoRefreshText} loading={loading} downloading={jobs.downloadingJobID === jobs.job.id} onRefresh={() => void jobs.refreshJobByID(jobs.job!.id)} onDownload={() => void jobs.downloadJob(jobs.job!.id)} /></Section>}
  </PageCanvas>
}
