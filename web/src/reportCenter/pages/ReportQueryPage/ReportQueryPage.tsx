import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
import DatePicker from 'antd/es/date-picker'
import dayjs from 'dayjs'
import { ChevronDown, ChevronLeft, ChevronRight, Download, Filter, Play, Plus, Square, Trash2 } from 'lucide-react'
import { Button, Dialog, FeedbackState, FilterToolbar, PageCanvas, PageHeader, Section, StatusTag, type StatusTagTone } from '../../../ui'
import { cancelReportRun, createReportExport, createReportRun, getReportDownloads, getReportExport, getReportExportDownload, isRetryableReportPollFailure, queryReportResults, getReportRun, getReportRunContract, reportPollRetryDelay, type ReportCenterClient } from '../../api'
import { buildNewReportRunState, canBindReportExport, canRetryReportExportBinding, canStartNewReportRun, initialReportParameterValues, isReportExportSettled, terminalReportRunStatuses, visibleReportParameters } from '../../queryParameters'
import { buildReportConditions, editableReportConditionValue, initialReportConditionValues, isReportInputListType, orderedReportInputEntries } from '../../refCursorConfig'
import type { ReportExport, ReportFilterOperator, ReportInputField, ReportParameter, ReportResultColumn, ReportResultFilter, ReportResultPage, ReportResultQuery, ReportRun, ReportRunContract, ReportSummary, ReportQueryInputs } from '../../types'
import { ReportResultTable } from '../../components/ReportResultTable/ReportResultTable'
import { ReportRecordDetailDrawer, type RecordDetailSelection } from '../../components/ReportRecordDetailDrawer/ReportRecordDetailDrawer'
import { ReportRelationSelect } from '../../components/ReportRelationSelect/ReportRelationSelect'
import { ReportRuntimeQueryEditor } from '../../components/ReportQueryDesigner/ReportRuntimeQueryEditor'
import { normalizeRuntimeQueryInputs, validateRuntimeQueryInputs } from '../../components/ReportQueryDesigner/runtimeFilters'
import { ReportFieldDetailDrawer } from '../../components/ReportFieldDetailDrawer/ReportFieldDetailDrawer'
import { ReportInputQuerySelect } from '../../components/ReportInputQuerySelect/ReportInputQuerySelect'
import { useReportCatalog } from '../../useReportCatalog'
import design from '../../reportDesign.module.css'
import styles from './ReportQueryPage.module.css'

const emptyResultQuery: ReportResultQuery = { filters: [], sort: [] }
const reportExportPollIntervalMs = 1800
const maxReadyCleanupPolls = 8
const maxReadyCleanupPollIntervalMs = 10000

export function ReportQueryPage({ client, navigation, canExport = true }: { client: ReportCenterClient; navigation?: ReactNode; canExport?: boolean }) {
  const query = useMemo(() => ({ limit: 100 }), [])
  const { items, loading, loadingMore, error, hasMore, reload, loadMore } = useReportCatalog(client, query, getReportDownloads)
  const published = items.filter((report) => report.status === 'ACTIVE')
  const reportsByCategory = groupReportsByCategory(published)
  const defaultReportId = published[0]?.id ?? 0
  const [selectedId, setSelectedId] = useState('')
  const [contract, setContract] = useState<ReportRunContract | null>(null)
  const [values, setValues] = useState<Record<string, unknown>>({})
  const [contractState, setContractState] = useState<{ loading: boolean; error: string }>({ loading: false, error: '' })
  const [run, setRun] = useState<ReportRun | null>(null)
  const [result, setResult] = useState<ReportResultPage | null>(null)
  const [cursorHistory, setCursorHistory] = useState<string[]>([''])
  const [cursorIndex, setCursorIndex] = useState(0)
  const [operation, setOperation] = useState<{ busy: boolean; error: string }>({ busy: false, error: '' })
  const [reportExport, setReportExport] = useState<ReportExport | null>(null)
  const [resultQuery, setResultQuery] = useState<ReportResultQuery>(emptyResultQuery)
  const [appliedQuery, setAppliedQuery] = useState<ReportResultQuery>(emptyResultQuery)
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [parametersOpen, setParametersOpen] = useState(true)
  const [cancelState, setCancelState] = useState({ open: false, busy: false, error: '' })
  const [queryInputs, setQueryInputs] = useState<ReportQueryInputs>({ where: { logic: 'AND', items: [] }, orderBy: [] })
  const [submittedInputFingerprint, setSubmittedInputFingerprint] = useState('')
  const [recordSelection, setRecordSelection] = useState<RecordDetailSelection | null>(null)
  const selectSnapshot = contract?.executionMode === 'SELECT_SNAPSHOT'
  const purgeAfterExport = contract?.executionPolicy?.purgeAfterExport ?? true
  const automaticExport = canExport && (!selectSnapshot || contract.executionPolicy?.exportPolicy === 'AUTO')
  const exportSettled = (item: ReportExport | null) => isReportExportSettled(item, purgeAfterExport)
  const [detailColumn, setDetailColumn] = useState<ReportResultColumn | null>(null)
  const pollAbortRef = useRef<AbortController | null>(null)
  const keepRunningRef = useRef<HTMLButtonElement>(null)
  const relationDependencyValues = useMemo(() => {
    const conditions: Record<string, unknown> = {}
    for (const [code, field] of orderedReportInputEntries(contract?.inputSchema ?? {})) {
      const normalized = buildReportConditions({ [code]: field }, values)
      if (normalized.ok) Object.assign(conditions, normalized.conditions)
    }
    return conditions
  }, [contract, values])

  function replacePollController() {
    pollAbortRef.current?.abort()
    const controller = new AbortController()
    pollAbortRef.current = controller
    return controller
  }

  function ownsPoll(controller: AbortController) {
    return !controller.signal.aborted && pollAbortRef.current === controller
  }

  useEffect(() => () => pollAbortRef.current?.abort(), [])

  useEffect(() => {
    if (!selectedId && defaultReportId > 0) setSelectedId(String(defaultReportId))
  }, [defaultReportId, selectedId])

  useEffect(() => {
    pollAbortRef.current?.abort()
    setContract(null)
    setQueryInputs({ where: { logic: 'AND', items: [] }, orderBy: [] })
    setSubmittedInputFingerprint('')
    setRecordSelection(null)
    setRun(null)
    setResult(null)
    setReportExport(null)
    setResultQuery(emptyResultQuery)
    setAppliedQuery(emptyResultQuery)
    setCursorHistory([''])
    setCursorIndex(0)
    setOperation({ busy: false, error: '' })
    setCancelState({ open: false, busy: false, error: '' })
    if (!selectedId) return
    const reportId = Number(selectedId)
    const controller = new AbortController()
    setContractState({ loading: true, error: '' })
    void getReportRunContract(client, reportId, controller.signal).then((response) => {
      if (controller.signal.aborted) return
      if (!response.ok) {
        setContractState({ loading: false, error: response.error })
        return
      }
      setContract(response.data)
      setValues(response.data.jsonInput ? initialReportConditionValues(response.data.inputSchema) : initialReportParameterValues(response.data.parameters))
      setContractState({ loading: false, error: '' })
    })
    return () => controller.abort()
  }, [client, selectedId])

  async function submitRun(event: FormEvent) {
    event.preventDefault()
    if (!contract || operation.busy) return
    if (selectSnapshot) {
      const error = validateRuntimeQueryInputs(contract, queryInputs)
      if (error) { setOperation({ busy: false, error }); return }
    }
    let runInput: Record<string, unknown>
    if (contract.jsonInput) {
      const normalized = buildReportConditions(contract.inputSchema, values)
      if (!normalized.ok) { setOperation({ busy: false, error: normalized.error }); return }
      runInput = normalized.conditions
    } else {
      const normalized = buildRunParameters(contract.parameters, values)
      if (!normalized.ok) { setOperation({ busy: false, error: normalized.error }); return }
      runInput = normalized.parameters
    }
    setOperation({ busy: true, error: '' })
    const controller = replacePollController()
    const response = await createReportRun(client, contract.definitionId, runInput, contract.executionMode, contract.jsonInput, controller.signal, selectSnapshot ? normalizeRuntimeQueryInputs(queryInputs) : undefined, selectSnapshot && run ? crypto.randomUUID() : undefined)
    if (!ownsPoll(controller)) return
    if (!response.ok) {
      setOperation({ busy: false, error: response.error })
      return
    }
    setSubmittedInputFingerprint(JSON.stringify({ values, queryInputs: normalizeRuntimeQueryInputs(queryInputs) }))
    setRun(response.data)
    setResult(null)
    setReportExport(null)
    setCursorHistory([''])
    setCursorIndex(0)
    await pollRun(response.data.id, controller)
  }

  async function pollRun(runId: number, controller = replacePollController()) {
    let transientFailures = 0
    while (ownsPoll(controller)) {
      const response = await getReportRun(client, runId, controller.signal)
      if (!ownsPoll(controller)) return
      if (!response.ok) {
        if (isRetryableReportPollFailure(response)) {
          transientFailures++
          await wait(reportPollRetryDelay(response, transientFailures), controller.signal)
          continue
        }
        setOperation({ busy: false, error: response.error })
        return
      }
      transientFailures = 0
      setRun(response.data)
      if (terminalReportRunStatuses.has(response.data.status)) {
        setOperation({ busy: false, error: response.data.errorMessage })
        if (response.data.resultAvailable) {
          await loadResults(runId, emptyResultQuery, '', 0, controller.signal)
        }
        if (!ownsPoll(controller)) return
        if (automaticExport && canBindReportExport(response.data)) await startExport(runId, controller)
        return
      }
      if (automaticExport && canBindReportExport(response.data)) {
        await startExport(runId, controller)
        return
      }
      await wait(1500, controller.signal)
    }
  }

  async function resumeRun() {
    if (!run || operation.busy) return
    setOperation({ busy: true, error: '' })
    await pollRun(run.id)
  }

  async function loadResults(runId: number, query: ReportResultQuery, cursor: string, pageIndex: number, signal?: AbortSignal) {
    setOperation((current) => ({ ...current, busy: true, error: '' }))
    const response = await queryReportResults(client, runId, query, cursor, 100, signal)
    if (signal?.aborted) return
    if (!response.ok) {
      if (!signal?.aborted) setOperation({ busy: false, error: response.error })
      return
    }
    setResult(response.data)
    setRun(response.data.run)
    setCursorIndex(pageIndex)
    setOperation({ busy: false, error: '' })
  }

  async function nextPage() {
    if (!run || !result?.pagination.nextCursor) return
    const controller = replacePollController()
    const nextIndex = cursorIndex + 1
    const nextHistory = [...cursorHistory.slice(0, nextIndex), result.pagination.nextCursor]
    setCursorHistory(nextHistory)
    await loadResults(run.id, appliedQuery, result.pagination.nextCursor, nextIndex, controller.signal)
  }

  async function previousPage() {
    if (!run || cursorIndex === 0) return
    const controller = replacePollController()
    const previousIndex = cursorIndex - 1
    await loadResults(run.id, appliedQuery, cursorHistory[previousIndex], previousIndex, controller.signal)
  }

  async function applyResultQuery() {
    if (!run?.resultAvailable || operation.busy || reportExport) return
    const normalized = normalizeResultQuery(resultQuery)
    const controller = replacePollController()
    setAppliedQuery(normalized)
    setCursorHistory([''])
    setCursorIndex(0)
    await loadResults(run.id, normalized, '', 0, controller.signal)
  }

  async function cancelRun() {
    if (!run?.canCancel || cancelState.busy) return
    const controller = replacePollController()
    setCancelState({ open: true, busy: true, error: '' })
    const response = await cancelReportRun(client, run.id, controller.signal)
    if (!ownsPoll(controller)) return
    if (!response.ok) {
      setCancelState({ open: true, busy: false, error: response.error })
      await pollRun(run.id, controller)
      return
    }
    setCancelState({ open: false, busy: false, error: '' })
    setRun(response.data)
    if (terminalReportRunStatuses.has(response.data.status)) {
      setOperation({ busy: false, error: response.data.errorMessage })
      return
    }
    await pollRun(response.data.id, controller)
  }

  async function startExport(runID = run?.id, controller = replacePollController()) {
    if (!canExport || !runID || reportExport) return
    if (!ownsPoll(controller)) return
    setOperation({ busy: true, error: '' })
    const response = await createReportExport(client, runID, selectSnapshot ? emptyResultQuery : appliedQuery, controller.signal)
    if (!ownsPoll(controller)) return
    if (!response.ok) {
      setOperation({ busy: false, error: response.error })
      return
    }
    setReportExport(response.data)
    await pollExport(response.data.id, controller)
  }

  async function pollExport(exportId: number, controller = replacePollController()) {
    let readyCleanupPolls = 0
    let transientFailures = 0
    while (ownsPoll(controller)) {
      const response = await getReportExport(client, exportId, controller.signal)
      if (!ownsPoll(controller)) return
      if (!response.ok) {
        if (isRetryableReportPollFailure(response)) {
          transientFailures++
          await wait(reportPollRetryDelay(response, transientFailures), controller.signal)
          continue
        }
        setOperation({ busy: false, error: response.error })
        return
      }
      transientFailures = 0
      setReportExport(response.data)
      if (exportSettled(response.data)) {
        setOperation({ busy: false, error: response.data.errorMessage })
        return
      }
      if (response.data.status === 'READY') {
        readyCleanupPolls++
        if (readyCleanupPolls >= maxReadyCleanupPolls) {
          setOperation({ busy: false, error: '文件已生成，但结果仍在清理中。你可以先下载文件，稍后重新检查。' })
          return
        }
      } else {
        readyCleanupPolls = 0
      }
      const delay = response.data.status === 'READY'
        ? Math.min(reportExportPollIntervalMs * (2 ** Math.max(0, readyCleanupPolls - 1)), maxReadyCleanupPollIntervalMs)
        : reportExportPollIntervalMs
      await wait(delay, controller.signal)
    }
  }

  async function resumeExportPolling() {
    if (!reportExport || operation.busy || exportSettled(reportExport)) return
    setOperation({ busy: true, error: '' })
    await pollExport(reportExport.id)
  }

  async function downloadExport() {
    if (!reportExport?.canDownload) return
    const response = await getReportExportDownload(client, reportExport.id)
    if (!response.ok) {
      setOperation({ busy: false, error: response.error })
      return
    }
    window.location.assign(response.data.url)
  }

  function startNewRun() {
    if (!contract || !run || !canStartNewReportRun(run.status, reportExport, operation.busy, purgeAfterExport)) return
    pollAbortRef.current?.abort()
    pollAbortRef.current = null
    const next = buildNewReportRunState(contract.parameters)
    setRun(next.run)
    setResult(next.result)
    setReportExport(next.reportExport)
    setResultQuery(next.resultQuery)
    setAppliedQuery(next.appliedQuery)
    setCursorHistory(next.cursorHistory)
    setCursorIndex(next.cursorIndex)
    setFiltersOpen(next.filtersOpen)
    setParametersOpen(next.parametersOpen)
    setValues(contract.jsonInput ? initialReportConditionValues(contract.inputSchema) : next.values)
    setQueryInputs({ where: { logic: 'AND', items: [] }, orderBy: [] })
    setSubmittedInputFingerprint('')
    setOperation(next.operation)
  }

  const frozen = Boolean(run) && !selectSnapshot
  const conditionsChanged = Boolean(selectSnapshot && run && submittedInputFingerprint !== JSON.stringify({ values, queryInputs: normalizeRuntimeQueryInputs(queryInputs) }))
  const canStartNewRun = run ? canStartNewReportRun(run.status, reportExport, operation.busy, purgeAfterExport) : false
  return (
    <PageCanvas density="compact" className={`${design.surface} ${design.page}`}>
      {navigation}
      <PageHeader eyebrow="QUERY WORKSPACE" title="报表查询与下载" description="选择业务报表，按条件查询，再将本次结果保存为 Excel。" actions={run ? <div className={styles.pageActions}>{run.canCancel ? <button type="button" onClick={() => setCancelState({ open: true, busy: false, error: '' })}><Square aria-hidden="true" />取消生成</button> : null}<button type="button" onClick={startNewRun} disabled={!canStartNewRun}><Plus aria-hidden="true" />重新选择</button></div> : undefined} />
      <ol className={styles.querySteps} aria-label="报表查询流程"><li data-active={!contract}><span>01</span><strong>选择报表</strong><small>使用已发布版本</small></li><li data-active={Boolean(contract && !run)}><span>02</span><strong>填写条件</strong><small>确定本次查询范围</small></li><li data-active={Boolean(run)}><span>03</span><strong>查看与导出</strong><small>保留本次查询结果</small></li></ol>
      <FilterToolbar className={styles.reportSelector} summary={run ? <StatusTag tone={runTone(run)}>{runLabel(run.status)}</StatusTag> : <StatusTag tone="neutral">{contractState.loading ? '正在读取条件…' : contract ? '准备查询' : '选择已发布报表'}</StatusTag>}>
        <div className={styles.catalogSelector}><label className={styles.selector}>选择报表<select value={selectedId} onChange={(event) => { pollAbortRef.current?.abort(); setSelectedId(event.currentTarget.value) }} disabled={loading || published.length === 0}><option value="">请选择已上线报表</option>{reportsByCategory.map(([category, reports]) => <optgroup label={category} key={category}>{reports.map((report) => <option value={report.id} key={report.id}>{report.name}</option>)}</optgroup>)}</select></label>{hasMore ? <button type="button" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? '正在加载…' : '加载更多'}</button> : null}</div>
      </FilterToolbar>
      {selectedId ? <Section className={styles.conditionsPanel} title="查询条件" actions={<button type="button" aria-expanded={parametersOpen} onClick={() => setParametersOpen((open) => !open)}><ChevronDown className={parametersOpen ? styles.chevronOpen : undefined} aria-hidden="true" />{parametersOpen ? '收起' : '展开'}</button>}>
        {parametersOpen ? <>
        {contractState.loading ? <FeedbackState kind="loading" title="正在加载下载条件" /> : null}
        {contractState.error ? <FeedbackState kind="error" title="下载条件加载失败" description={contractState.error} /> : null}
        {contract ? <form className={styles.parameterForm} onSubmit={(event) => void submitRun(event)}>{contract.jsonInput ? orderedReportInputEntries(contract.inputSchema).map(([code, field]) => <ConditionField client={client} reportId={contract.definitionId} versionId={contract.versionId} code={code} disabled={frozen || operation.busy} field={field} dependencyValues={relationDependencyValues} key={code} value={values[code]} onChange={(value) => setValues((current) => ({ ...current, [code]: value }))} />) : visibleReportParameters(contract.parameters).map((parameter) => <ParameterField disabled={frozen || operation.busy} key={parameter.code} parameter={parameter} value={values[parameter.code]} onChange={(value) => setValues((current) => ({ ...current, [parameter.code]: value }))} />)}{selectSnapshot ? <ReportRuntimeQueryEditor contract={contract} value={queryInputs} disabled={operation.busy} onChange={setQueryInputs} /> : null}<div className={styles.runActions}><Button variant="primary" type="submit" disabled={frozen || operation.busy || (!selectSnapshot && !canExport)}><Play aria-hidden="true" />{selectSnapshot ? run ? '重新查询' : '查询报表' : '生成下载文件'}</Button></div></form> : null}
        </> : <div className={styles.collapsedParameters}>{contract ? `${reportConditionCount(contract)} 个筛选条件${frozen ? ' · 本次运行条件已冻结' : ''}` : '筛选区已收起'}</div>}
      </Section> : null}
      {conditionsChanged ? <p className={styles.queryChanged} role="status">条件已修改，请重新查询。下方仍展示上次运行快照。</p> : null}{!selectSnapshot && contract && !canExport ? <p role="status">当前存储过程报表需要导出权限，请联系管理员授权。</p> : null}
      {operation.error ? <FeedbackState kind="error" title="操作未完成" description={operation.error} action={reportExport && !exportSettled(reportExport) ? <button type="button" onClick={() => void resumeExportPolling()}>{reportExport.status === 'READY' ? '重新检查清理状态' : '恢复文件状态查询'}</button> : automaticExport && canRetryReportExportBinding(run, reportExport) ? <button type="button" onClick={() => void startExport(run?.id)}>重新获取下载文件</button> : run && !terminalReportRunStatuses.has(run.status) ? <button type="button" onClick={() => void resumeRun()}>恢复状态查询</button> : undefined} /> : null}
      {loading ? <FeedbackState kind="loading" title="正在加载可下载报表" /> : error ? <FeedbackState kind="error" title="可下载报表加载失败" description={error} action={<button type="button" onClick={reload}>重试</button>} /> : null}
      {run ? <Section title={selectSnapshot ? '查询结果' : '下载文件'} actions={<div className={styles.resultActions}>{reportExport ? <StatusTag tone={exportTone(reportExport)}>{reportExport.status === 'READY' && purgeAfterExport && !reportExport.purgedAt ? '可下载 · 清理中' : exportLabel(reportExport.status)}</StatusTag> : <StatusTag tone={runTone(run)}>{runLabel(run.status)}</StatusTag>}{selectSnapshot && canExport && run.resultAvailable && !reportExport ? <Button variant="primary" disabled={operation.busy} onClick={() => void startExport(run.id)}><Download aria-hidden="true" />导出本次结果</Button> : null}{canExport && reportExport?.canDownload ? <Button variant="primary" onClick={() => void downloadExport()}><Download aria-hidden="true" />下载文件</Button> : null}</div>} flush>
        {operation.busy && !reportExport?.canDownload ? <FeedbackState kind="loading" title={reportExport ? '正在生成 Excel' : '正在生成报表'} description={reportExport ? exportProgress(reportExport) : undefined} /> : null}
        {selectSnapshot ? <p className={styles.resultMeta}>运行 #{run.id} · 版本 #{run.versionId} · {run.rowCount.toLocaleString('zh-CN')} 行 · 生成 {run.finishedAt ? new Date(run.finishedAt).toLocaleString('zh-CN') : '进行中'} · 有效至 {run.resultExpiresAt ? new Date(run.resultExpiresAt).toLocaleString('zh-CN') : '待确认'}{!run.resultAvailable && run.resultExpiresAt && Date.parse(run.resultExpiresAt) <= Date.now() ? ' · 结果已到期，请重新查询' : ''}</p> : null}
        {result ? <details open={selectSnapshot || undefined} className={styles.optionalPreview}><summary>{selectSnapshot ? '数据明细' : '查看结果预览（可选）'}</summary><div>
          {contract?.executionMode === 'TABLE_SNAPSHOT' ? <ResultQueryToolbar page={result} query={resultQuery} open={filtersOpen} disabled={operation.busy || Boolean(reportExport)} onToggle={() => setFiltersOpen((value) => !value)} onChange={setResultQuery} onApply={() => void applyResultQuery()} /> : null}
          <ReportResultTable page={result} onInspect={setDetailColumn} onRecord={setRecordSelection} />
          <div className={styles.pagination}><span>第 {cursorIndex + 1} 页 · 每页 {result.pagination.pageSize} 行</span><div><button type="button" onClick={() => void previousPage()} disabled={operation.busy || cursorIndex === 0}><ChevronLeft aria-hidden="true" />上一页</button><button type="button" onClick={() => void nextPage()} disabled={operation.busy || !result.pagination.hasMore}>下一页<ChevronRight aria-hidden="true" /></button></div></div>
        </div></details> : null}
      </Section> : null}
      {!run && !loading && !error ? <section className={styles.waitingResult} aria-label="结果预览"><FeedbackState kind="empty" title={published.length ? '查询结果将在这里展开' : '暂无可查询的报表'} description={published.length ? '完成上方条件后开始查询，结果、关联详情与导出入口会集中显示在这里。' : '请先在报表工作室完成设计并发布，再回来查询数据。'} /></section> : null}
      <Dialog open={cancelState.open && Boolean(run?.canCancel)} role="alertdialog" title="确认取消报表运行" description={run ? `运行 #${run.id} 的取消请求提交后不能撤回。` : undefined} closeDisabled={cancelState.busy} closeOnBackdrop={!cancelState.busy} initialFocusRef={keepRunningRef} onClose={() => setCancelState({ open: false, busy: false, error: '' })} footer={<><button ref={keepRunningRef} type="button" disabled={cancelState.busy} onClick={() => setCancelState({ open: false, busy: false, error: '' })}>继续运行</button><Button variant="danger" disabled={cancelState.busy} onClick={() => void cancelRun()}>{cancelState.busy ? '正在提交…' : '确认取消运行'}</Button></>}><p className={styles.cancelWarning}>系统会请求 Oracle 侧停止当前执行；已经写入结果表的数据仍按运行清理策略处理。</p>{cancelState.error ? <p className={styles.cancelError} role="alert">{cancelState.error}</p> : null}</Dialog>
      <ReportRecordDetailDrawer client={client} selection={recordSelection} onClose={() => setRecordSelection(null)} />
      <ReportFieldDetailDrawer column={detailColumn} onClose={() => setDetailColumn(null)} />
    </PageCanvas>
  )
}

function ParameterField({ parameter, value, disabled, onChange }: { parameter: ReportParameter; value: unknown; disabled: boolean; onChange: (value: unknown) => void }) {
  const id = `report-parameter-${parameter.code}`
  const hint = [`{{${parameter.code}}}`, parameter.oracleType, parameter.required ? '必填' : '选填', parameter.sensitive ? '敏感' : ''].filter(Boolean).join(' · ')
  if (parameter.controlType === 'CHECKBOX') return <label className={styles.field} htmlFor={id}><span>{parameter.label}{parameter.required ? ' *' : ''}</span><select id={id} required={parameter.required} disabled={disabled} value={value === true ? 'true' : value === false ? 'false' : ''} onChange={(event) => onChange(event.currentTarget.value === '' ? '' : event.currentTarget.value === 'true')}><option value="">{parameter.required ? '请选择' : '不传入 / NULL'}</option><option value="true">是</option><option value="false">否</option></select><small>{hint}</small></label>
  if (parameter.controlType === 'SELECT' || parameter.controlType === 'MULTI_SELECT') return <label className={styles.field} htmlFor={id}><span>{parameter.label}{parameter.required ? ' *' : ''}</span><select id={id} multiple={parameter.controlType === 'MULTI_SELECT'} required={parameter.required} disabled={disabled} value={parameter.controlType === 'MULTI_SELECT' ? toStringArray(value) : String(value ?? '')} onChange={(event) => onChange(parameter.controlType === 'MULTI_SELECT' ? Array.from(event.currentTarget.selectedOptions, (option) => option.value) : event.currentTarget.value)}><option value="" disabled={parameter.required}>请选择</option>{parameter.allowedValues.map((option) => <option value={String(option)} key={String(option)}>{String(option)}</option>)}</select><small>{hint}</small></label>
  if (parameter.controlType === 'DATE' || parameter.controlType === 'DATETIME') return <ReportDateField id={id} label={`${parameter.label}${parameter.required ? ' *' : ''}`} hint={parameter.errorMessage || hint} value={value} disabled={disabled} required={parameter.required} dateTime={parameter.controlType === 'DATETIME'} onChange={onChange} />
  const type = parameter.sensitive ? 'password' : 'text'
  const rules = parameter.validation
  const common = { id, required: parameter.required, disabled, value: String(value ?? ''), minLength: safeIntegerRule(rules.minLength), maxLength: parameter.maxLength ?? safeIntegerRule(rules.maxLength), pattern: typeof rules.pattern === 'string' ? rules.pattern : undefined, onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(event.currentTarget.value) }
  return <label className={styles.field} htmlFor={id}><span>{parameter.label}{parameter.required ? ' *' : ''}</span>{parameter.controlType === 'TEXTAREA' ? <textarea {...common} rows={3} /> : <input {...common} type={type} inputMode={parameter.controlType === 'NUMBER' ? 'decimal' : undefined} />}<small>{parameter.errorMessage || hint}</small></label>
}

function ConditionField({ client, reportId, versionId, code, field, dependencyValues, value, disabled, onChange }: { client: ReportCenterClient; reportId: number; versionId: number; code: string; field: ReportInputField; dependencyValues: Record<string, unknown>; value: unknown; disabled: boolean; onChange: (value: unknown) => void }) {
  const id = `report-condition-${code}`
  const label = `${field.displayName}${field.required ? ' *' : ''}`
  const list = isReportInputListType(field.type)
  const hint = [code, field.type, field.format, field.required ? '必填' : '选填'].filter(Boolean).join(' · ')
  if (field.relationId) return <label className={styles.field} htmlFor={id}><span>{label}</span><ReportRelationSelect client={client} reportId={reportId} versionId={versionId} relationId={field.relationId} keyColumnId={field.relationKeyColumnId} dependencyValues={dependencyValues} inputId={id} value={value} onChange={onChange} disabled={disabled} required={field.required} multiple={list} /><small>{hint}</small></label>
	if (field.queryName) return <label className={styles.field} htmlFor={id}><span>{label}</span><ReportInputQuerySelect client={client} reportId={reportId} conditionCode={code} inputId={id} value={value} required={field.required} disabled={disabled} multiple={list} numeric={field.type === 'number' || field.type === 'list[number]'} onChange={onChange} /><small>{hint} · 显示 name，提交 id{list ? ' 列表' : ''}</small></label>
  if (field.allowedValues?.length) {
    const options = field.allowedValues.map((item, index) => ({ key: String(index), value: editableReportConditionValue(item, { ...field, allowedValues: undefined }), label: displayConditionOption(item) }))
    const selected = list ? options.filter((option) => (Array.isArray(value) ? value : []).some((item) => comparableConditionValue(item) === comparableConditionValue(option.value))).map((option) => option.key) : options.find((option) => comparableConditionValue(option.value) === comparableConditionValue(value))?.key ?? ''
    return <label className={styles.field} htmlFor={id}><span>{label}</span><select id={id} multiple={list} required={field.required} disabled={disabled} value={selected} onChange={(event) => onChange(list ? Array.from(event.currentTarget.selectedOptions, (option) => option.value).filter(Boolean).map((key) => options[Number(key)]?.value).filter((item) => item !== undefined) : event.currentTarget.value === '' ? '' : options[Number(event.currentTarget.value)]?.value)}>{list ? null : <option value="" disabled={field.required}>请选择</option>}{options.map((option) => <option value={option.key} key={option.key}>{option.label}</option>)}</select><small>{hint}</small></label>
  }
  if (list || field.type === 'json') return <label className={styles.field} htmlFor={id}><span>{label}</span><textarea id={id} rows={3} required={field.required} disabled={disabled} value={Array.isArray(value) || (field.type === 'json' && value && typeof value === 'object') ? JSON.stringify(value) : String(value ?? '')} placeholder={list ? '["a","b"]' : '{"key":"value"}'} onChange={(event) => onChange(event.currentTarget.value)} /><small>{hint}</small></label>
  if (field.type === 'bool' || field.control === 'CHECKBOX') return <label className={styles.field} htmlFor={id}><span>{label}</span><select id={id} required={field.required} disabled={disabled} value={value === true ? 'true' : value === false ? 'false' : ''} onChange={(event) => onChange(event.currentTarget.value === '' ? '' : event.currentTarget.value === 'true')}><option value="">请选择</option><option value="true">是</option><option value="false">否</option></select><small>{hint}</small></label>
	if (field.control === 'DATE' || field.control === 'DATETIME') return <ReportDateField id={id} label={label} hint={hint} value={value} disabled={disabled} required={field.required} dateTime={field.control === 'DATETIME'} onChange={onChange} />
  const common = { id, required: field.required, disabled, value: String(value ?? ''), placeholder: field.example === undefined ? undefined : String(field.example), onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(event.currentTarget.value) }
  return <label className={styles.field} htmlFor={id}><span>{label}</span>{field.control === 'TEXTAREA' ? <textarea {...common} rows={3} /> : <input {...common} type="text" inputMode={field.type === 'number' || field.control === 'NUMBER' ? 'decimal' : undefined} />}<small>{hint}</small></label>
}

function ReportDateField({ id, label, hint, value, disabled, required, dateTime, onChange }: { id: string; label: string; hint: string; value: unknown; disabled: boolean; required: boolean; dateTime: boolean; onChange: (value: unknown) => void }) {
	const text = String(value ?? '')
	const parsed = text ? dayjs(text) : null
	const pickerValue = parsed?.isValid() ? parsed : null
	return <label className={`${styles.field} ${styles.dateField}`} htmlFor={id}><span>{label}</span><DatePicker id={id} aria-required={required} className={styles.datePicker} allowClear={!required} disabled={disabled} format={dateTime ? 'YYYY-MM-DD HH:mm:ss' : 'YYYY-MM-DD'} placeholder={dateTime ? '选择日期和时间' : '选择日期'} showTime={dateTime ? { format: 'HH:mm:ss' } : false} value={pickerValue} onChange={(next) => onChange(next ? next.format(dateTime ? 'YYYY-MM-DDTHH:mm:ss' : 'YYYY-MM-DD') : '')} /><small>{hint}</small></label>
}

function ResultQueryToolbar({ page, query, open, disabled, onToggle, onChange, onApply }: { page: ReportResultPage; query: ReportResultQuery; open: boolean; disabled: boolean; onToggle: () => void; onChange: (query: ReportResultQuery) => void; onApply: () => void }) {
	const filterable = page.columns.filter((column) => column.filterable && column.allowedOperators.length > 0)
	const sortable = page.columns.filter((column) => column.sortable)
	return <div className={styles.queryToolbar}>
		<div className={styles.querySummary}><button type="button" onClick={onToggle} aria-expanded={open}><Filter aria-hidden="true" />结果筛选<ChevronDown aria-hidden="true" className={open ? styles.expandedIcon : undefined} /></button><span>{query.filters.length} 个条件 · {query.sort.length ? '已排序' : '默认顺序'}{disabled ? ' · 导出条件已冻结' : ''}</span></div>
		{open ? <div className={styles.queryEditor}>
			{query.filters.map((filter, index) => <ResultFilterField key={`${filter.field}-${index}`} columns={filterable} filter={filter} disabled={disabled} onChange={(next) => onChange({ ...query, filters: replaceAt(query.filters, index, next) })} onDelete={() => onChange({ ...query, filters: query.filters.filter((_, itemIndex) => itemIndex !== index) })} />)}
			<div className={styles.queryControls}><button type="button" disabled={disabled || filterable.length === 0 || query.filters.length >= 8} onClick={() => { const column = filterable[0]; if (column) onChange({ ...query, filters: [...query.filters, { field: column.fieldId, operator: column.allowedOperators[0] ?? 'EQ', value: '' }] }) }}><Plus aria-hidden="true" />添加条件</button><label>排序字段<select value={query.sort[0]?.field ?? ''} disabled={disabled} onChange={(event) => onChange({ ...query, sort: event.currentTarget.value ? [{ field: event.currentTarget.value, direction: query.sort[0]?.direction ?? 'ASC' }] : [] })}><option value="">默认顺序</option>{sortable.map((column) => <option key={column.fieldId} value={column.fieldId}>{column.header}</option>)}</select></label><label>方向<select value={query.sort[0]?.direction ?? 'ASC'} disabled={disabled || query.sort.length === 0} onChange={(event) => onChange({ ...query, sort: query.sort.length ? [{ ...query.sort[0], direction: event.currentTarget.value as 'ASC' | 'DESC' }] : [] })}><option value="ASC">升序</option><option value="DESC">降序</option></select></label><Button variant="primary" disabled={disabled} onClick={onApply}>应用筛选</Button></div>
		</div> : null}
	</div>
}

function ResultFilterField({ columns, filter, disabled, onChange, onDelete }: { columns: ReportResultPage['columns']; filter: ReportResultFilter; disabled: boolean; onChange: (filter: ReportResultFilter) => void; onDelete: () => void }) {
	const column = columns.find((item) => item.fieldId === filter.field) ?? columns[0]
	if (!column) return null
	const operator = column.allowedOperators.includes(filter.operator) ? filter.operator : column.allowedOperators[0]
	const noValue = operator === 'IS_NULL' || operator === 'IS_NOT_NULL'
	return <div className={styles.filterRow}><label>字段<select value={column.fieldId} disabled={disabled} onChange={(event) => { const next = columns.find((item) => item.fieldId === event.currentTarget.value); if (next) onChange({ field: next.fieldId, operator: next.allowedOperators[0] ?? 'EQ', value: '' }) }}>{columns.map((item) => <option key={item.fieldId} value={item.fieldId}>{item.header}</option>)}</select></label><label>条件<select value={operator} disabled={disabled} onChange={(event) => onChange({ field: column.fieldId, operator: event.currentTarget.value as ReportFilterOperator, value: '' })}>{column.allowedOperators.map((item) => <option key={item} value={item}>{operatorLabel(item)}</option>)}</select></label>{noValue ? <span className={styles.noValue}>无需填写值</span> : <label>值<input disabled={disabled} value={filterValue(filter.value)} placeholder={operator === 'IN' || operator === 'NOT_IN' || operator === 'BETWEEN' ? '多个值用英文逗号分隔' : '请输入筛选值'} onChange={(event) => onChange({ ...filter, field: column.fieldId, operator, value: event.currentTarget.value })} /></label>}<button type="button" aria-label={`删除 ${column.header} 条件`} disabled={disabled} onClick={onDelete}><Trash2 aria-hidden="true" /></button></div>
}

function normalizeResultQuery(query: ReportResultQuery): ReportResultQuery {
	return { sort: query.sort, filters: query.filters.map((filter) => {
		if (filter.operator === 'IS_NULL' || filter.operator === 'IS_NOT_NULL') return { field: filter.field, operator: filter.operator }
		const text = String(filter.value ?? '').trim()
		if (filter.operator === 'IN' || filter.operator === 'NOT_IN' || filter.operator === 'BETWEEN') return { ...filter, value: text.split(',').map((item) => item.trim()).filter(Boolean) }
		return { ...filter, value: typedFilterValue(text) }
	}) }
}
function typedFilterValue(value: string): unknown { if (/^-?\d+(?:\.\d+)?$/.test(value)) return value; if (value === 'true') return true; if (value === 'false') return false; return value }
function filterValue(value: unknown) { return Array.isArray(value) ? value.join(',') : String(value ?? '') }
function replaceAt<T>(items: T[], index: number, value: T) { return items.map((item, itemIndex) => itemIndex === index ? value : item) }
function operatorLabel(operator: ReportFilterOperator) { return ({ EQ: '等于', NE: '不等于', GT: '大于', GTE: '大于等于', LT: '小于', LTE: '小于等于', IN: '属于集合', NOT_IN: '不属于集合', IS_NULL: '为空', IS_NOT_NULL: '不为空', CONTAINS: '包含', STARTS_WITH: '开头为', BETWEEN: '区间' })[operator] }

function buildRunParameters(parameters: ReportParameter[], values: Record<string, unknown>): { ok: true; parameters: Record<string, unknown> } | { ok: false; error: string } {
  const result: Record<string, unknown> = {}
  for (const parameter of visibleReportParameters(parameters)) {
    const value = values[parameter.code]
    if (value === '' || value === undefined || (Array.isArray(value) && value.length === 0)) {
      if (parameter.required) return { ok: false, error: `${parameter.label} 为必填筛选条件。` }
      continue
    }
    if ((parameter.logicalType === 'integer' || parameter.logicalType === 'decimal') && (typeof value !== 'string' || !/^-?\d+(?:\.\d+)?$/.test(value))) return { ok: false, error: `${parameter.label} 必须填写有效数字。` }
    if (parameter.logicalType === 'integer' && typeof value === 'string' && !/^-?\d+$/.test(value)) return { ok: false, error: `${parameter.label} 必须填写整数。` }
    if (parameter.logicalType === 'datetime' && typeof value === 'string') {
      const dateTime = zonedDateTimeToRFC3339(value, parameter.timezone)
      if (!dateTime) return { ok: false, error: `${parameter.label} 不是有效日期时间，或时区配置无效。` }
      result[parameter.code] = dateTime
      continue
    }
    if (parameter.logicalType === 'json' && typeof value === 'string') {
      try { result[parameter.code] = JSON.parse(value) as unknown } catch { return { ok: false, error: `${parameter.label} 必须填写有效 JSON。` } }
      continue
    }
    result[parameter.code] = value
  }
  return { ok: true, parameters: result }
}
function toStringArray(value: unknown) { return Array.isArray(value) ? value.map(String) : [] }
function groupReportsByCategory(reports: ReportSummary[]) {
	const groups = new Map<string, ReportSummary[]>()
	for (const report of reports) {
		const category = report.category || '未分类'
		groups.set(category, [...(groups.get(category) ?? []), report])
	}
	return [...groups.entries()].sort(([left], [right]) => left.localeCompare(right, 'zh-CN'))
}
function reportConditionCount(contract: ReportRunContract) { return (contract.jsonInput ? Object.keys(contract.inputSchema).length : visibleReportParameters(contract.parameters).length) + (contract.executionMode === 'SELECT_SNAPSHOT' ? contract.allowedFilters?.length ?? 0 : 0) }
function displayConditionOption(value: unknown) { return typeof value === 'string' ? value : JSON.stringify(value) }
function comparableConditionValue(value: unknown) { return JSON.stringify(value) }
function wait(milliseconds: number, signal: AbortSignal) { return new Promise<void>((resolve) => { if (signal.aborted) { resolve(); return } const finish = () => { window.clearTimeout(timer); signal.removeEventListener('abort', finish); resolve() }; const timer = window.setTimeout(finish, milliseconds); signal.addEventListener('abort', finish, { once: true }); if (signal.aborted) finish() }) }
function runTone(run: ReportRun): StatusTagTone { return ['SUCCEEDED', 'EXPORTED', 'RESULT_PURGED'].includes(run.status) ? 'success' : run.status === 'FAILED' || run.status === 'CANCELLED' ? 'danger' : run.status === 'UNKNOWN' || run.status === 'RECONCILING' || run.status === 'SUPERSEDED' ? 'warning' : 'running' }
function runLabel(status: ReportRun['status']) { return ({ QUEUED: '等待执行', RUNNING: '正在执行', CANCEL_REQUESTED: '正在取消', SUCCEEDED: '运行成功', FAILED: '运行失败', CANCELLED: '已取消', UNKNOWN: '状态待确认', RECONCILING: '正在对账', EXPORTING: '正在导出', EXPORTED: '已导出', RESULT_PURGING: '正在清理结果', RESULT_PURGED: '结果已清理', SUPERSEDED: '已被新运行替代' })[status] }
function exportTone(item: ReportExport): StatusTagTone { return item.status === 'READY' ? 'success' : item.status === 'FAILED' || item.status === 'CANCELLED' || item.status === 'EXPIRED' ? 'danger' : 'running' }
function exportLabel(status: ReportExport['status']) { return ({ PENDING: '等待导出', RUNNING: '生成并上传 Excel', READY: '文件就绪', FAILED: '导出失败', CANCELLED: '导出取消', EXPIRED: '文件已过期' })[status] }
function exportProgress(item: ReportExport) { return `${exportLabel(item.status)} · 已处理 ${item.processedRows.toLocaleString('zh-CN')} 行${item.currentSheet ? ` · ${item.currentSheet}` : ''}` }
function safeIntegerRule(value: unknown) { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined }
function zonedDateTimeToRFC3339(value: string, timezone: string) {
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) {
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime()) ? '' : parsed.toISOString()
  }
  const matched = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value)
  if (!matched) return ''
  const zone = timezone || Intl.DateTimeFormat().resolvedOptions().timeZone
  try {
    const expected = { year: Number(matched[1]), month: Number(matched[2]), day: Number(matched[3]), hour: Number(matched[4]), minute: Number(matched[5]), second: Number(matched[6] ?? 0) }
    let instant = Date.UTC(expected.year, expected.month - 1, expected.day, expected.hour, expected.minute, expected.second)
    const formatter = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const parts = Object.fromEntries(formatter.formatToParts(new Date(instant)).filter((part) => part.type !== 'literal').map((part) => [part.type, Number(part.value)]))
      const rendered = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second)
      const target = Date.UTC(expected.year, expected.month - 1, expected.day, expected.hour, expected.minute, expected.second)
      instant += target - rendered
    }
    const confirmed = Object.fromEntries(formatter.formatToParts(new Date(instant)).filter((part) => part.type !== 'literal').map((part) => [part.type, Number(part.value)]))
    if (confirmed.year !== expected.year || confirmed.month !== expected.month || confirmed.day !== expected.day || confirmed.hour !== expected.hour || confirmed.minute !== expected.minute || confirmed.second !== expected.second) return ''
    return new Date(instant).toISOString()
  } catch { return '' }
}
