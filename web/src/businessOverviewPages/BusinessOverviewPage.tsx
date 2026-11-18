import { useCallback, useEffect, useRef, useState } from 'react'
import { CalendarDays, Maximize2, Minimize2, Save } from 'lucide-react'
import {
  emptyBusinessSummary,
  businessOverviewMallsPath,
  businessOverviewPaymentsPath,
  mergeBusinessOverviewMalls,
  parseBusinessOverviewMalls,
  parseBusinessOverviewPayments,
  recordPaymentDetail,
  recordTotal,
  shiftBusinessDate,
  type CashierID,
  type BusinessOverviewMall,
} from '../businessOverview'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { PageCanvas } from '../ui'
import styles from './BusinessOverviewPage.module.css'

const cashierOptions: Array<{ id: CashierID; label: string }> = [
  { id: 'all', label: '全部' },
]

type MallLoadState = 'loading' | 'success' | 'error'
type PaymentLoadState = 'idle' | 'loading' | 'success' | 'error'

export function BusinessOverviewPage({ client }: { client: WorkspaceApiClient }) {
  const today = shanghaiDate(new Date())
  const yesterday = shiftBusinessDate(today, -1)
  const fullscreenSurfaceRef = useRef<HTMLDivElement>(null)
  const mallRequestSequence = useRef(0)
  const mallController = useRef<AbortController | null>(null)
  const paymentRequestSequence = useRef(0)
  const paymentController = useRef<AbortController | null>(null)
  const mallsRef = useRef<BusinessOverviewMall[]>([])
  const [malls, setMalls] = useState<BusinessOverviewMall[]>([])
  const [mallCode, setMallCode] = useState('')
  const [mallLoadState, setMallLoadState] = useState<MallLoadState>('loading')
  const [mallError, setMallError] = useState('')
  const [nextAfterID, setNextAfterID] = useState(0)
  const [selectedDate, setSelectedDate] = useState(today)
  const [cashierID, setCashierID] = useState<CashierID>('all')
  const [summary, setSummary] = useState(() => emptyBusinessSummary(today))
  const [paymentLoadState, setPaymentLoadState] = useState<PaymentLoadState>('idle')
  const [paymentError, setPaymentError] = useState('')
  const [savedMessage, setSavedMessage] = useState('')
  const [fullscreenError, setFullscreenError] = useState('')
  const [isFullscreen, setIsFullscreen] = useState(false)
  const selectedMall = malls.find((mall) => mall.mallCode === mallCode)

  const saveReconciliation = useCallback(() => {
    if (!selectedMall || paymentLoadState !== 'success') return
    setSavedMessage(`${selectedMall.nameCn} ${selectedDate} 的对账数据已保存（模拟）`)
  }, [paymentLoadState, selectedDate, selectedMall])

  function changeDate(date: string) {
    if (date === selectedDate) return
    setSelectedDate(date)
    setSummary(emptyBusinessSummary(date))
    setPaymentLoadState(mallCode ? 'loading' : 'idle')
    setPaymentError('')
    setSavedMessage('')
  }

  function changeCashier(value: string) {
    if (!cashierOptions.some((option) => option.id === value)) return
    setCashierID(value as CashierID)
    setSavedMessage('')
  }

  function changeMall(value: string) {
    if (!malls.some((mall) => mall.mallCode === value)) return
    setMallCode(value)
    setSummary(emptyBusinessSummary(selectedDate))
    setPaymentLoadState('loading')
    setPaymentError('')
    setSavedMessage('')
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

  const loadPayments = useCallback(async (date: string, code: string) => {
    const sequence = ++paymentRequestSequence.current
    paymentController.current?.abort()
    const controller = new AbortController()
    paymentController.current = controller
    setSummary(emptyBusinessSummary(date))
    setPaymentLoadState('loading')
    setPaymentError('')
    try {
      const response = await client(businessOverviewPaymentsPath(date, code), { method: 'GET', showResult: false, silentLoading: true, signal: controller.signal })
      if (controller.signal.aborted || sequence !== paymentRequestSequence.current) return
      if (!response.ok) {
        setPaymentLoadState('error')
        setPaymentError(response.status === 403 ? '当前账号无权查询该商场营业数据。' : '营业数据加载失败，请稍后重试。')
        return
      }
      const parsed = parseBusinessOverviewPayments(response.data, date, code)
      if (!parsed) {
        setPaymentLoadState('error')
        setPaymentError('营业数据响应格式不正确，请联系管理员。')
        return
      }
      setSummary(parsed)
      setPaymentLoadState('success')
    } catch {
      if (controller.signal.aborted || sequence !== paymentRequestSequence.current) return
      setPaymentLoadState('error')
      setPaymentError('营业数据加载异常，请检查网络后重试。')
    } finally {
      if (paymentController.current === controller) paymentController.current = null
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
      paymentController.current?.abort()
      setSummary(emptyBusinessSummary(selectedDate))
      setPaymentLoadState('idle')
      setPaymentError('')
      return
    }
    void loadPayments(selectedDate, mallCode)
    return () => paymentController.current?.abort()
  }, [loadPayments, mallCode, selectedDate])

  useEffect(() => {
    const handleShortcut = (event: KeyboardEvent) => {
      if (event.key !== 'F2') return
      event.preventDefault()
      saveReconciliation()
    }
    window.addEventListener('keydown', handleShortcut)
    return () => window.removeEventListener('keydown', handleShortcut)
  }, [saveReconciliation])

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
        <header className={styles.titleBar}><h1>营业概况</h1></header>

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

        <section className={styles.overviewPanel} aria-label={`${selectedMall?.nameCn ?? '当前商场'}营业概况日结核对`} aria-busy={paymentLoadState === 'loading'}>
        <div className={styles.filterBar}>
          <strong className={styles.filterTitle}>销售日期：</strong>
          <div className={styles.quickDates} aria-label="快捷选择销售日期">
            <button aria-pressed={selectedDate === today} className={selectedDate === today ? styles.active : ''} type="button" onClick={() => changeDate(today)}>今天</button>
            <button aria-pressed={selectedDate === yesterday} className={selectedDate === yesterday ? styles.active : ''} type="button" onClick={() => changeDate(yesterday)}>昨天</button>
          </div>
          <label className={styles.dateField}>
            <span>指定日期</span>
            <span className={styles.dateInputWrap}>
              <input type="date" value={selectedDate} onChange={(event) => changeDate(event.currentTarget.value)} />
              <CalendarDays aria-hidden="true" />
            </span>
          </label>
          <button className={styles.saveButton} type="button" onClick={saveReconciliation} disabled={!selectedMall || paymentLoadState !== 'success'}><Save aria-hidden="true" />保存 F2</button>
          <label className={styles.cashierField}>
            <span>收银机号：</span>
            <select value={cashierID} onChange={(event) => changeCashier(event.currentTarget.value)} disabled>
              {cashierOptions.map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
            </select>
          </label>
        </div>

        {savedMessage ? <div className={styles.savedMessage} role="status" aria-live="polite">{savedMessage}</div> : null}
        {paymentLoadState === 'loading' ? <div className={styles.mallEmpty} role="status">正在加载营业数据…</div> : null}
        {paymentLoadState === 'error' ? <div className={styles.mallError} role="alert"><span>{paymentError}</span><button type="button" onClick={() => void loadPayments(selectedDate, mallCode)}>重新加载</button></div> : null}

        <div className={styles.summaryArea}>
          <div className={styles.amountSummary}>
            <div className={styles.unsettledAmount}><strong>{formatAmount(summary.unsettledAmount)}</strong><span>末日结金额（元）</span></div>
            <div className={styles.channelAmounts}>
              <div><span>门店： </span><strong>{formatAmount(summary.storeAmount)}（元）</strong></div>
              <div><span>云仓： </span><strong>{formatAmount(summary.cloudAmount)}（元）</strong></div>
            </div>
            <div className={styles.reconciliationTip}>
              <span className={styles.tipIcon}><img src="/business-overview-reconciliation-tip.png" alt="" /></span>
              <p><span>亲，日结时请核对收款金额</span><br />是否正确哦～</p>
            </div>
          </div>

          <div className={styles.paymentSummary}>
            <div className={styles.paymentList}>
              {summary.payments.length > 0 ? summary.payments.map((payment) => (
                <div className={styles.paymentRow} key={payment.name}><span>{payment.name}</span><output>{formatAmount(payment.amount)}</output></div>
              )) : <div className={styles.emptyPayments}>所选日期暂无收款数据</div>}
            </div>
            <div className={styles.adjustments}>
              <MetricField label="当日实存" value={summary.actualAmount} />
              <MetricField label="公关费用" value={summary.publicExpense} />
              <MetricField label="预订金额" value={summary.depositAmount} />
              <MetricField label="预售金额" value={summary.presaleAmount} />
            </div>
          </div>
        </div>

        <section className={styles.reconciliationSection} aria-labelledby="reconciliation-title">
          <h2 id="reconciliation-title">门店对账单</h2>
          <div className={styles.tableWrap} role="region" aria-label="门店对账记录，可横向滚动" tabIndex={0}>
            <table>
              <thead><tr><th scope="col">销售日期</th><th scope="col">收银机号</th><th scope="col">总计</th><th scope="col">实收金额</th><th scope="col">末日结金额</th><th scope="col">日结明细</th></tr></thead>
              <tbody>
                {summary.records.length > 0 ? summary.records.map((record) => (
                  <tr key={record.id}>
                    <td>{record.date}</td><td>{record.cashierName}</td><td>{formatAmount(recordTotal(record))}</td><td>{formatAmount(record.received)}</td><td>{formatAmount(record.unsettled)}</td><td>{recordPaymentDetail(record)}</td>
                  </tr>
                )) : <tr><td className={styles.emptyTable} colSpan={6}>当前筛选条件下暂无门店对账记录</td></tr>}
              </tbody>
            </table>
          </div>
        </section>
        </section>
      </PageCanvas>
    </div>
  )
}

function MetricField({ label, value }: { label: string; value: number }) {
  return <label className={styles.metricField}><span>{label}</span><output>{formatAmount(value)}</output></label>
}

function formatAmount(value: number) {
  return new Intl.NumberFormat('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 2 }).format(value)
}

function shanghaiDate(date: Date) {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(date)
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day}`
}
