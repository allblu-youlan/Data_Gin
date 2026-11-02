import { formatShanghaiDateTime } from '../dateTime.js'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { ClientResponse } from '../api/client'
import { buildExcelMatchJobListQuery, normalizeMonitoringPageNumber, parseMonitoringPage, type MonitoringPagination } from '../monitoringRecords'
import {
  canDownloadExcelJob,
  excelJobPollMaxAttempts,
  excelJobStatusLabel,
  isExcelJobActive,
  readDataField,
  readList,
  readObject,
  replaceExcelJobHistoryItem,
  submitExcelDownloadForm,
  type ExcelMatchJob,
  type ExcelMatchJobLog,
} from './excelPageSupport'
import type { ExcelPageClient } from './useExcelUploads'
import { nextExcelJobPollState } from './excelJobPolling'

type UseExcelJobsOptions = {
  section: 'jobs' | 'schemes' | 'write'
  client: ExcelPageClient
  token: string
  refreshVersion: number
  setLoading: (value: boolean) => void
  setResult: (value: ClientResponse | null) => void
}

function requestErrorMessage(response: ClientResponse, fallback: string) {
  return response.error?.message || fallback
}

export function useExcelJobs({ section, client, token, refreshVersion, setLoading, setResult }: UseExcelJobsOptions) {
  const [jobID, setJobID] = useState('')
  const [job, setJob] = useState<ExcelMatchJob | null>(null)
  const [jobDetailOpen, setJobDetailOpen] = useState(false)
  const jobDetailTriggerRef = useRef<HTMLButtonElement | null>(null)
  const [jobHistory, setJobHistory] = useState<ExcelMatchJob[]>([])
  const [jobLogs, setJobLogs] = useState<ExcelMatchJobLog[]>([])
  const [trackingJobID, setTrackingJobID] = useState<number | null>(null)
  const [autoRefreshText, setAutoRefreshText] = useState('')
  const [downloadingJobID, setDownloadingJobID] = useState<number | null>(null)
  const [jobQuery, setJobQuery] = useState('')
  const [jobStatus, setJobStatus] = useState('all')
  const [jobOperation, setJobOperation] = useState('all')
  const [appliedJobHistoryFilters, setAppliedJobHistoryFilters] = useState({ keyword: '', status: '', operation: '' })
  const [jobHistoryPage, setJobHistoryPage] = useState(1)
  const [jobHistoryPagination, setJobHistoryPagination] = useState<MonitoringPagination | null>(null)
  const [jobHistoryLoading, setJobHistoryLoading] = useState(false)
  const [jobHistoryError, setJobHistoryError] = useState('')
  const [jobHistoryReloadVersion, setJobHistoryReloadVersion] = useState(0)
  const jobHistoryRequestRef = useRef<AbortController | null>(null)

  const applyJobResult = useCallback((result: ClientResponse, options: { track?: boolean } = {}) => {
    const nextJob = readObject<ExcelMatchJob>(result, 'job')
    if (nextJob) {
      setJob(nextJob)
      setJobID(String(nextJob.id))
      setJobHistory((current) => replaceExcelJobHistoryItem(current, nextJob))
      if (options.track !== false) setTrackingJobID(isExcelJobActive(nextJob) ? nextJob.id : null)
    }
    setJobLogs(readList<ExcelMatchJobLog>(result, 'logs'))
    return nextJob
  }, [])

  const loadJobHistory = useCallback(async () => {
    jobHistoryRequestRef.current?.abort()
    const controller = new AbortController()
    jobHistoryRequestRef.current = controller
    setJobHistoryLoading(true)
    setJobHistoryError('')
    const query = buildExcelMatchJobListQuery({ page: jobHistoryPage, pageSize: 20, ...appliedJobHistoryFilters })
    try {
      const response = await client(`/v1/excel-match-jobs?${query}`, { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true })
      if (controller.signal.aborted) return
      const parsed = response.ok ? parseMonitoringPage<ExcelMatchJob>(response.data, 'jobs') : null
      if (parsed) {
        const nextPage = normalizeMonitoringPageNumber(jobHistoryPage, parsed.pagination.totalPages)
        if (nextPage !== jobHistoryPage) {
          setJobHistoryPage(nextPage)
          return
        }
        setJobHistory(parsed.list)
        setJobHistoryPagination(parsed.pagination)
        return
      }
      const legacyItems = readDataField(response.data, 'jobs')
      if (response.ok && Array.isArray(legacyItems)) {
        const pageSize = 20
        if (jobHistoryPage !== 1) {
          setJobHistoryPage(1)
          return
        }
        setJobHistory(legacyItems.slice(0, pageSize) as ExcelMatchJob[])
        setJobHistoryPagination({ page: 1, pageSize, total: legacyItems.length, totalPages: legacyItems.length ? 1 : 0 })
        setJobHistoryError('当前服务暂不支持 Excel 任务分页或筛选，已显示未筛选的兼容数据。')
        return
      }
      setJobHistoryError(response.error?.message || 'Excel 任务历史暂时不可用，请稍后重试。')
    } finally {
      if (!controller.signal.aborted) setJobHistoryLoading(false)
    }
  }, [appliedJobHistoryFilters, client, jobHistoryPage])

  useEffect(() => {
    if (!token || section !== 'jobs') return
    void loadJobHistory()
    return () => jobHistoryRequestRef.current?.abort()
  }, [jobHistoryReloadVersion, loadJobHistory, refreshVersion, section, token])

  useEffect(() => {
    if (section !== 'jobs') setJobDetailOpen(false)
  }, [section])

  const refreshJobByID = useCallback(async (id: number, options: { silent?: boolean; track?: boolean; signal?: AbortSignal } = {}) => {
    if (!options.silent) setLoading(true)
    try {
      const nextResult = await client(`/v1/excel-match-jobs/${id}`, { method: 'GET', signal: options.signal, showResult: false, silentLoading: true })
      if (options.signal?.aborted) return null
      if (!options.silent && !nextResult.ok) setResult(nextResult)
      if (nextResult.ok) {
        applyJobResult(nextResult, { track: options.track })
        if (!options.silent) await loadJobHistory()
        return readObject<ExcelMatchJob>(nextResult, 'job')
      }
      if (options.silent) setAutoRefreshText(`自动刷新失败：${requestErrorMessage(nextResult, '请稍后重试。')}`)
    } catch {
      if (options.signal?.aborted) return null
      if (!options.silent) setResult({ ok: false, status: 0, data: { message: '查询 Excel 任务失败，请稍后重试。' } })
      else setAutoRefreshText('自动刷新失败，请稍后重试。')
    } finally {
      if (!options.silent) setLoading(false)
    }
    return null
  }, [applyJobResult, client, loadJobHistory, setLoading, setResult])

  async function openJobDetail(id: number, trigger: HTMLButtonElement) {
    jobDetailTriggerRef.current = trigger
    const nextJob = await refreshJobByID(id)
    if (nextJob) setJobDetailOpen(true)
    else trigger.focus()
  }

  useEffect(() => {
    if (!token || !trackingJobID) return
    let cancelled = false
    let consecutiveFailures = 0
    let timer: number | null = null
    let inFlight = false
    const pollingState = { resumeWhenVisible: false }
    const controller = new AbortController()
    const isPageVisible = () => document.visibilityState !== 'hidden'
    const clearScheduledRefresh = () => { if (timer !== null) { window.clearTimeout(timer); timer = null } }
    const stopPolling = (message: string) => { clearScheduledRefresh(); setAutoRefreshText(message); setTrackingJobID(null) }
    const scheduleRefresh = (delayMilliseconds: number) => {
      clearScheduledRefresh()
      if (cancelled || document.visibilityState === 'hidden' || inFlight) return
      timer = window.setTimeout(() => { timer = null; void refreshTrackedJob() }, delayMilliseconds)
    }
    const refreshTrackedJob = async () => {
      if (cancelled || document.visibilityState === 'hidden') return
      if (inFlight) { pollingState.resumeWhenVisible = true; return }
      inFlight = true
      let nextDelay: number | null = null
      try {
        const nextJob = await refreshJobByID(trackingJobID, { silent: true, signal: controller.signal })
        if (cancelled || controller.signal.aborted) return
        const poll = nextExcelJobPollState(consecutiveFailures, Boolean(nextJob), excelJobPollMaxAttempts)
        consecutiveFailures = poll.consecutiveFailures
        if (!nextJob) {
          const delayMilliseconds = poll.delayMilliseconds
          if (poll.stop) { stopPolling(`自动刷新连续失败 ${excelJobPollMaxAttempts} 次，已停止，请手动查询任务状态。`); return }
          setAutoRefreshText(`自动刷新失败，将在 ${Math.ceil(delayMilliseconds / 1000)} 秒后重试。`)
          nextDelay = delayMilliseconds
          return
        }
        setAutoRefreshText(`自动刷新中：任务 #${nextJob.id}，${excelJobStatusLabel(nextJob.status)}，${formatShanghaiDateTime(new Date())}`)
        if (!isExcelJobActive(nextJob)) { setTrackingJobID(null); void loadJobHistory(); return }
        nextDelay = poll.delayMilliseconds
      } finally {
        inFlight = false
        if (pollingState.resumeWhenVisible && !cancelled && !controller.signal.aborted && isPageVisible()) { pollingState.resumeWhenVisible = false; scheduleRefresh(0) }
        else if (nextDelay !== null) scheduleRefresh(nextDelay)
      }
    }
    void refreshTrackedJob()
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'hidden') { clearScheduledRefresh(); setAutoRefreshText('页面已隐藏，任务自动刷新已暂停。'); return }
      if (inFlight) { pollingState.resumeWhenVisible = true; return }
      scheduleRefresh(0)
    }
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => { cancelled = true; clearScheduledRefresh(); controller.abort(); document.removeEventListener('visibilitychange', handleVisibilityChange) }
  }, [loadJobHistory, refreshJobByID, token, trackingJobID])

  function registerCreatedJob(result: ClientResponse, openDetail: boolean) {
    const createdJob = applyJobResult(result)
    if (createdJob && openDetail) setJobDetailOpen(true)
    setAutoRefreshText('')
    setJobHistoryReloadVersion((version) => version + 1)
    return createdJob
  }

  async function downloadJob(targetID?: number) {
    const id = targetID ?? Number(jobID || job?.id)
    if (!id) { setResult({ ok: false, status: 0, data: '请输入任务 ID' }); return }
    const targetJob = job?.id === id ? job : jobHistory.find((item) => item.id === id) ?? null
    if (targetJob && !canDownloadExcelJob(targetJob)) { setResult({ ok: false, status: 0, data: targetJob.download_message || '结果文件尚未上传到OSS，上传成功后才能下载，请稍后刷新任务状态' }); return }
    setDownloadingJobID(id)
    setResult({ ok: true, status: 0, data: `正在提交任务 ${id} 的下载请求，浏览器会接管文件下载。` })
    try {
      submitExcelDownloadForm(id, token)
      setResult({ ok: true, status: 0, data: `任务 ${id} 下载请求已提交，请查看浏览器下载栏。` })
      await loadJobHistory()
    } catch (error) {
      setResult({ ok: false, status: 0, data: error instanceof Error ? error.message : String(error) })
    } finally {
      setDownloadingJobID(null)
    }
  }

  return {
    jobID, setJobID, job, jobDetailOpen, setJobDetailOpen, jobDetailTriggerRef, jobHistory, jobLogs,
    trackingJobID, autoRefreshText, downloadingJobID, jobQuery, setJobQuery, jobStatus, setJobStatus,
    jobOperation, setJobOperation, setAppliedJobHistoryFilters, jobHistoryPage, setJobHistoryPage,
    jobHistoryPagination, jobHistoryLoading, jobHistoryError, setJobHistoryReloadVersion,
    loadJobHistory, refreshJobByID, openJobDetail, registerCreatedJob, downloadJob,
  }
}
