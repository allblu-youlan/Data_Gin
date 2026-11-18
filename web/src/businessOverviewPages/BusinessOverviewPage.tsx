import { useCallback, useEffect, useRef, useState } from 'react'
import { CalendarDays, Maximize2, Minimize2, RefreshCcw } from 'lucide-react'
import {
  businessOverviewMallsPath,
  mergeBusinessOverviewMalls,
  parseBusinessOverviewMalls,
  shiftBusinessDate,
  type BusinessOverviewMall,
} from '../businessOverview'
import { isSalesDate, loadPushedSalesSummary, pushedSalesSummaryDescription, shanghaiSalesDate, type SalesSummary } from '../salesSummary/contracts'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { PageCanvas } from '../ui'
import styles from './BusinessOverviewPage.module.css'

type MallLoadState = 'loading' | 'success' | 'error'
type SummaryLoadState = 'idle' | 'loading' | 'success' | 'error'

export function BusinessOverviewPage({ client }: { client: WorkspaceApiClient }) {
  const today = shanghaiSalesDate()
  const yesterday = shiftBusinessDate(today, -1)
  const fullscreenSurfaceRef = useRef<HTMLDivElement>(null)
  const mallRequestSequence = useRef(0)
  const mallController = useRef<AbortController | null>(null)
  const summaryRequestSequence = useRef(0)
  const summaryController = useRef<AbortController | null>(null)
  const mallsRef = useRef<BusinessOverviewMall[]>([])
  const [malls, setMalls] = useState<BusinessOverviewMall[]>([])
  const [mallCode, setMallCode] = useState('')
  const [mallLoadState, setMallLoadState] = useState<MallLoadState>('loading')
  const [mallError, setMallError] = useState('')
  const [nextAfterID, setNextAfterID] = useState(0)
  const [selectedDate, setSelectedDate] = useState(today)
  const [summary, setSummary] = useState<SalesSummary | null>(null)
  const [summaryLoadState, setSummaryLoadState] = useState<SummaryLoadState>('idle')
  const [summaryError, setSummaryError] = useState('')
  const [fullscreenError, setFullscreenError] = useState('')
  const [isFullscreen, setIsFullscreen] = useState(false)
  const selectedMall = malls.find((mall) => mall.mallCode === mallCode)

  function changeDate(date: string) {
    if (!isSalesDate(date) || date === selectedDate) return
    setSelectedDate(date)
    setSummary(null)
    setSummaryLoadState(mallCode ? 'loading' : 'idle')
    setSummaryError('')
  }

  function changeMall(value: string) {
    if (!malls.some((mall) => mall.mallCode === value)) return
    setMallCode(value)
    setSummary(null)
    setSummaryLoadState('loading')
    setSummaryError('')
  }

  const loadMalls = useCallback(async (afterID = 0) => {
    const sequence = ++mallRequestSequence.current
    mallController.current?.abort()
    const controller = new AbortController()
    mallController.current = controller
    setMallLoadState('loading')
    setMallError('')
    try {
      const response = await client(businessOverviewMallsPath(afterID), { method: 'GET', showResult: false, silentLoading: true, signal: controller.signal })
      if (controller.signal.aborted || sequence !== mallRequestSequence.current) return
      if (!response.ok) {
        setMallLoadState('error')
        setMallError(response.status === 403 ? '当前账号无权读取商场列表。' : '商场列表加载失败，请稍后重试。')
        return
      }
      const parsed = parseBusinessOverviewMalls(response.data)
      if (!parsed || (parsed.items.length === 50 && parsed.nextAfterId <= afterID)) {
        setMallLoadState('error')
        setMallError('商场列表响应格式不正确，请联系管理员。')
        return
      }
      const nextMalls = afterID > 0 ? mergeBusinessOverviewMalls(mallsRef.current, parsed.items) : parsed.items
      mallsRef.current = nextMalls
      setMalls(nextMalls)
      setMallCode((current) => nextMalls.some((mall) => mall.mallCode === current) ? current : nextMalls[0]?.mallCode ?? '')
      setNextAfterID(parsed.items.length === 50 ? parsed.nextAfterId : 0)
      setMallLoadState('success')
    } catch {
      if (controller.signal.aborted || sequence !== mallRequestSequence.current) return
      setMallLoadState('error')
      setMallError('商场列表加载异常，请检查网络后重试。')
    } finally {
      if (mallController.current === controller) mallController.current = null
    }
  }, [client])

  const loadSummary = useCallback(async (date: string, code: string) => {
    const sequence = ++summaryRequestSequence.current
    summaryController.current?.abort()
    const controller = new AbortController()
    summaryController.current = controller
    setSummary(null)
    setSummaryLoadState('loading')
    setSummaryError('')
    try {
      const parsed = await loadPushedSalesSummary(client, code, { startDate: date, endDate: date }, controller.signal)
      if (controller.signal.aborted || sequence !== summaryRequestSequence.current) return
      setSummary(parsed)
      setSummaryLoadState('success')
    } catch (error) {
      if (controller.signal.aborted || sequence !== summaryRequestSequence.current) return
      setSummaryLoadState('error')
      setSummaryError(error instanceof Error ? error.message : '已推送业绩加载异常，请检查网络后重试。')
    } finally {
      if (summaryController.current === controller) summaryController.current = null
    }
  }, [client])

  const toggleFullscreen = useCallback(async () => {
    const surface = fullscreenSurfaceRef.current
    if (!surface) return
    setFullscreenError('')
    const exiting = document.fullscreenElement === surface
    try {
      if (exiting) {
        await document.exitFullscreen()
        return
      }
      await surface.requestFullscreen()
    } catch {
      setFullscreenError(exiting ? '无法退出全屏，请再次按 Esc。' : '无法进入全屏，请检查浏览器是否允许全屏显示。')
    }
  }, [])

  useEffect(() => {
    void loadMalls()
    return () => mallController.current?.abort()
  }, [loadMalls])

  useEffect(() => {
    if (!mallCode) {
      summaryController.current?.abort()
      setSummary(null)
      setSummaryLoadState('idle')
      setSummaryError('')
      return
    }
    void loadSummary(selectedDate, mallCode)
    return () => summaryController.current?.abort()
  }, [loadSummary, mallCode, selectedDate])

  useEffect(() => {
    const syncFullscreenState = () => {
      setIsFullscreen(document.fullscreenElement === fullscreenSurfaceRef.current)
      setFullscreenError('')
    }
    document.addEventListener('fullscreenchange', syncFullscreenState)
    return () => document.removeEventListener('fullscreenchange', syncFullscreenState)
  }, [])

  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || document.fullscreenElement !== fullscreenSurfaceRef.current) return
      void document.exitFullscreen().catch(() => setFullscreenError('无法退出全屏，请再次按 Esc 或点击退出全屏。'))
    }
    window.addEventListener('keydown', handleEscape)
    return () => window.removeEventListener('keydown', handleEscape)
  }, [])

  return (
    <div className={styles.fullscreenSurface} ref={fullscreenSurfaceRef}>
      <PageCanvas className={styles.page}>
        <header className={styles.titleBar}><h1>已推送业绩日报</h1></header>

        <div className={styles.pageControls}>
          {!isFullscreen && <label className={styles.mallField}>
            <span>选择商场：</span>
            <select name="businessOverviewMallCode" value={selectedMall ? mallCode : ''} onChange={(event) => changeMall(event.currentTarget.value)} disabled={malls.length === 0}>
              {malls.length === 0 && <option value="">{mallLoadState === 'loading' ? '正在加载商场' : '没有可选商场'}</option>}
              {malls.map((mall) => <option key={mall.id} value={mall.mallCode}>{mall.nameCn}（{mall.mallCode}）</option>)}
            </select>
          </label>}
          {!isFullscreen && nextAfterID > 0 && <button className={styles.loadMoreButton} type="button" onClick={() => void loadMalls(nextAfterID)} disabled={mallLoadState === 'loading'}>加载更多</button>}
          <button className={styles.fullscreenButton} type="button" onClick={() => void toggleFullscreen()} aria-pressed={isFullscreen}>
            {isFullscreen ? <Minimize2 aria-hidden="true" /> : <Maximize2 aria-hidden="true" />}
            {isFullscreen ? '退出全屏' : '全屏'}
          </button>
        </div>

        {!isFullscreen && mallLoadState === 'error' ? <div className={styles.mallError} role="alert"><span>{mallError}</span><button type="button" onClick={() => void loadMalls()}>重新加载</button></div> : null}
        {!isFullscreen && mallLoadState === 'success' && malls.length === 0 ? <div className={styles.mallEmpty} role="status">当前账号暂无可查看的商场。</div> : null}
        {fullscreenError ? <div className={styles.fullscreenError} role="alert">{fullscreenError}</div> : null}

        <section className={styles.overviewPanel} aria-label={`${selectedMall?.nameCn ?? '当前商场'}已推送业绩日报`} aria-busy={summaryLoadState === 'loading'}>
        <div className={styles.filterBar}>
          <strong className={styles.filterTitle}>推送日期：</strong>
          <div className={styles.quickDates} aria-label="快捷选择推送日期">
            <button aria-pressed={selectedDate === today} className={selectedDate === today ? styles.active : ''} type="button" onClick={() => changeDate(today)}>今天</button>
            <button aria-pressed={selectedDate === yesterday} className={selectedDate === yesterday ? styles.active : ''} type="button" onClick={() => changeDate(yesterday)}>昨天</button>
          </div>
          <label className={styles.dateField}>
            <span>指定推送日期</span>
            <span className={styles.dateInputWrap}>
              <input type="date" value={selectedDate} onChange={(event) => changeDate(event.currentTarget.value)} />
              <CalendarDays aria-hidden="true" />
            </span>
          </label>
          <button className={styles.refreshButton} type="button" onClick={() => void loadSummary(selectedDate, mallCode)} disabled={!selectedMall || summaryLoadState === 'loading'}><RefreshCcw aria-hidden="true" />刷新</button>
        </div>

        <p className={styles.summaryScope}>{pushedSalesSummaryDescription}</p>
        {summaryLoadState === 'loading' ? <div className={styles.mallEmpty} role="status">正在加载已推送业绩…</div> : null}
        {summaryLoadState === 'error' ? <div className={styles.mallError} role="alert"><span>{summaryError}</span><button type="button" onClick={() => void loadSummary(selectedDate, mallCode)}>重新加载</button></div> : null}

        <div className={styles.summaryArea}>
          <div className={styles.amountSummary}>
            <div className={styles.pushedAmount}><strong>{summary ? formatAmount(summary.metrics.salesAmount) : '—'}</strong><span>已推送净额（元）</span></div>
          </div>
          <div className={styles.pushedMetrics}>
            <MetricField label="已推送总笔数" value={summary?.metrics.pushedOrderCount} />
            <MetricField label="已推送销售件数" value={summary?.metrics.salesQuantity} />
            <MetricField label="已推送退货件数" value={summary?.metrics.returnQuantity} />
            <MetricField label="已推送净件数" value={summary?.metrics.totalQuantity} />
          </div>
        </div>

        <section className={styles.reconciliationSection} aria-labelledby="reconciliation-title">
          <h2 id="reconciliation-title">已推送业绩汇总</h2>
          <div className={styles.tableWrap} role="region" aria-label="已推送业绩汇总，可横向滚动" tabIndex={0}>
            <table>
              <thead><tr><th scope="col">推送日期</th><th scope="col">已推送净额（元）</th><th scope="col">已推送总笔数</th><th scope="col">已推送销售件数</th><th scope="col">已推送退货件数</th><th scope="col">已推送净件数</th></tr></thead>
              <tbody>
                {summary && summary.metrics.pushedOrderCount > 0 ? <tr>
                  <td>{selectedDate}</td><td>{formatAmount(summary.metrics.salesAmount)}</td><td>{formatQuantity(summary.metrics.pushedOrderCount)}</td><td>{formatQuantity(summary.metrics.salesQuantity)}</td><td>{formatQuantity(summary.metrics.returnQuantity)}</td><td>{formatQuantity(summary.metrics.totalQuantity)}</td>
                </tr> : <tr><td className={styles.emptyTable} colSpan={6}>{summaryLoadState === 'loading' ? '正在加载已推送业绩…' : summaryLoadState === 'error' ? '已推送业绩加载失败，请重试。' : summary ? '所选推送日期内暂无已推送业绩。' : '请选择商场查看已推送业绩。'}</td></tr>}
              </tbody>
            </table>
          </div>
        </section>
        </section>
      </PageCanvas>
    </div>
  )
}

function MetricField({ label, value }: { label: string; value: number | undefined }) {
  return <label className={styles.metricField}><span>{label}</span><output>{value === undefined ? '—' : formatQuantity(value)}</output></label>
}

function formatAmount(value: number) {
  return new Intl.NumberFormat('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value)
}

function formatQuantity(value: number) {
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 }).format(value)
}
