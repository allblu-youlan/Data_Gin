import { useEffect, useRef, useState } from 'react'
import {
  ChevronLeft, ChevronRight, CircleHelp, Cloud, CloudRain, CloudSnow, CloudUpload,
  Hexagon, LogOut, Menu, RefreshCcw, Sun, Volume2, Wifi, WifiOff,
} from 'lucide-react'
import { businessOverviewMallsPath, parseBusinessOverviewMalls, type BusinessOverviewMall } from '../businessOverview'
import { mallWeatherSkyconLabel } from '../mallWeather'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import {
  isSalesDate, parseSalesSummary, salesPeriodRange, salesSummaryPath,
  type SalesDateRange, type SalesPeriod, type SalesSummary, type SalesSummaryMetrics,
} from './contracts'
import styles from './SalesSummaryPage.module.css'

export type SalesSummaryPageProps = {
  client: WorkspaceApiClient
  username: string
  onNavigate?: (target: 'business-overview' | 'store-info' | 'report-center') => void
  onLogout?: () => void
  onOpenNavigation?: () => void
}

type MetricDefinition = { key: keyof SalesSummaryMetrics; label: string; format?: 'money' | 'percent' | 'ratio' }
const metrics: MetricDefinition[] = [
  { key: 'salesAmount', label: '销售金额', format: 'money' },
  { key: 'salesTarget', label: '销售指标' },
  { key: 'salesAchievementRate', label: '销售达成率', format: 'percent' },
  { key: 'averageOrderValue', label: '客单价' },
  { key: 'itemsPerOrder', label: '连带率', format: 'ratio' },
  { key: 'quantityTarget', label: '销量指标' },
  { key: 'conversionRate', label: '转化率' },
  { key: 'memberSalesRatio', label: '会员销售占比', format: 'percent' },
  { key: 'totalQuantity', label: '总件数' },
  { key: 'salesQuantity', label: '销售件数' },
  { key: 'returnQuantity', label: '退货件数' },
  { key: 'orderCount', label: '总笔数' },
  { key: 'averageItemPrice', label: '件单价' },
  { key: 'discountRate', label: '销售折扣率', format: 'percent' },
]

function formatMetric(value: number, format: MetricDefinition['format']) {
  if (format === 'money') return value.toLocaleString('en-US', { maximumFractionDigits: 2 })
  if (format === 'percent') return `${Number(value.toFixed(2))}%`
  if (format === 'ratio') return Number(value.toFixed(2)).toString()
  return Math.round(value).toString()
}

function WeatherIcon({ skycon }: { skycon: string }) {
  if (skycon.includes('RAIN')) return <CloudRain aria-hidden="true" />
  if (skycon.includes('SNOW')) return <CloudSnow aria-hidden="true" />
  if (skycon.startsWith('CLEAR')) return <Sun aria-hidden="true" />
  return <Cloud aria-hidden="true" />
}

export function SalesSummaryPage({ client, username, onNavigate, onLogout, onOpenNavigation }: SalesSummaryPageProps) {
  const surface = useRef<HTMLDivElement>(null)
  const [malls, setMalls] = useState<BusinessOverviewMall[]>([])
  const [mallCode, setMallCode] = useState('')
  const [period, setPeriod] = useState<SalesPeriod>('today')
  const [range, setRange] = useState<SalesDateRange>(() => salesPeriodRange('today'))
  const [draftRange, setDraftRange] = useState(range)
  const [datePickerOpen, setDatePickerOpen] = useState(false)
  const [dateError, setDateError] = useState('')
  const [summary, setSummary] = useState<SalesSummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [mallLoading, setMallLoading] = useState(true)
  const [mallError, setMallError] = useState('')
  const [summaryError, setSummaryError] = useState('')
  const [systemMessage, setSystemMessage] = useState('')
  const [refreshVersion, setRefreshVersion] = useState(0)
  const [mallRefreshVersion, setMallRefreshVersion] = useState(0)
  const [online, setOnline] = useState(() => navigator.onLine)
  const selectedMall = malls.find((mall) => mall.mallCode === mallCode)

  useEffect(() => {
    const controller = new AbortController()
    async function loadMalls() {
      setMallLoading(true)
      setMallError('')
      try {
        let afterID = 0
        const items: BusinessOverviewMall[] = []
        while (!controller.signal.aborted) {
          const response = await client(businessOverviewMallsPath(afterID, 200), { method: 'GET', showResult: false, silentLoading: true, signal: controller.signal })
          if (controller.signal.aborted) return
          if (!response.ok) throw new Error(response.status === 403 ? '当前账号无权读取商场列表。' : '商场列表加载失败，请点击刷新重试。')
          const page = parseBusinessOverviewMalls(response.data)
          if (!page) throw new Error('商场列表响应异常，请点击刷新重试。')
          items.push(...page.items)
          if (page.items.length < 200 || page.nextAfterId <= afterID) break
          afterID = page.nextAfterId
        }
        if (controller.signal.aborted) return
        setMalls(items)
        setMallCode((current) => items.some((mall) => mall.mallCode === current) ? current : items[0]?.mallCode ?? '')
        if (!items.length) setSystemMessage('当前账号暂无可访问的商场。')
      } catch (error) {
        if (!controller.signal.aborted) setMallError(error instanceof Error ? error.message : '商场列表加载失败，请点击刷新重试。')
      } finally {
        if (!controller.signal.aborted) setMallLoading(false)
      }
    }
    void loadMalls()
    return () => controller.abort()
  }, [client, mallRefreshVersion])

  useEffect(() => {
    if (!mallCode) { setLoading(false); return }
    const controller = new AbortController()
    async function loadSummary() {
      setLoading(true)
      setSummary(null)
      setSummaryError('')
      try {
        const response = await client(salesSummaryPath(mallCode, range), { method: 'GET', showResult: false, silentLoading: true, signal: controller.signal })
        if (controller.signal.aborted) return
        if (!response.ok) throw new Error(response.status === 403 ? '当前账号无权查询该商场销售数据。' : '销售数据加载失败，请点击刷新重试。')
        const parsed = parseSalesSummary(response.data, mallCode, range)
        if (!parsed) throw new Error('销售数据响应异常，请点击刷新重试。')
        setSummary(parsed)
      } catch (error) {
        if (!controller.signal.aborted) setSummaryError(error instanceof Error ? error.message : '销售数据加载失败，请点击刷新重试。')
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void loadSummary()
    return () => controller.abort()
  }, [client, mallCode, range, refreshVersion])

  useEffect(() => {
    const update = () => setOnline(navigator.onLine)
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    return () => { window.removeEventListener('online', update); window.removeEventListener('offline', update) }
  }, [])

  function refresh() {
    setSystemMessage('')
    if (mallError || !malls.length) setMallRefreshVersion((value) => value + 1)
    setRefreshVersion((value) => value + 1)
  }

  function selectPeriod(next: Exclude<SalesPeriod, 'custom'>) {
    setPeriod(next)
    setRange(salesPeriodRange(next))
    setDatePickerOpen(false)
    setSystemMessage('')
  }

  function applyDateRange() {
    if (!isSalesDate(draftRange.startDate) || !isSalesDate(draftRange.endDate) || draftRange.startDate > draftRange.endDate) {
      setDateError('请选择有效日期，开始日期不能晚于结束日期。')
      return
    }
    setPeriod('custom')
    setRange(draftRange)
    setDatePickerOpen(false)
    setDateError('')
  }

  async function toggleFullscreen() {
    try {
      if (document.fullscreenElement) await document.exitFullscreen()
      else await surface.current?.requestFullscreen()
    } catch {
      setSystemMessage('当前浏览器暂不支持全屏显示。')
    }
  }

  const weather = summary?.weather
  const weatherText = weather
    ? `${weather.temperatureMinC === null ? '—' : Math.round(weather.temperatureMinC)}°C ～ ${weather.temperatureMaxC === null ? '—' : Math.round(weather.temperatureMaxC)}°C ${mallWeatherSkyconLabel(weather.skycon).replace(/（.*）/, '')}`
    : '暂无天气数据'
  const message = mallError || summaryError || systemMessage
  const busy = loading || mallLoading

  return <div className={styles.page} ref={surface}>
    <header className={styles.header}>
      {onOpenNavigation && <button className={styles.mobileMenu} type="button" aria-label="打开导航" onClick={onOpenNavigation}><Menu /></button>}
      <div className={styles.identity}>
        <span className={styles.brand}>ALLBLU</span>
        <div className={styles.mallName}>
          <span>（{selectedMall?.nameCn ?? (mallLoading ? '加载中…' : '请选择商场')}）</span>
          <select aria-label="选择商场" value={mallCode} onChange={(event) => { setMallCode(event.target.value); setSystemMessage('') }} disabled={mallLoading || !malls.length}>
            {!malls.length && <option value="">{mallLoading ? '加载中…' : '暂无商场'}</option>}
            {malls.map((mall) => <option key={mall.id} value={mall.mallCode}>{mall.nameCn}</option>)}
          </select>
        </div>
        <span className={styles.storeCode}>{selectedMall?.mallCode ?? '—'}</span>
      </div>
      <nav className={styles.primaryNav} aria-label="门店导航">
        <button type="button" onClick={() => onNavigate?.('business-overview')}>收银</button>
        <button type="button" className={styles.primaryActive} aria-current="page">店长导航</button>
        <button type="button" onClick={() => onNavigate?.('store-info')}>店务管理</button>
        <button type="button" onClick={() => onNavigate?.('report-center')}>报表</button>
      </nav>
      <div className={styles.headerTools}>
        <button type="button" title="刷新销售数据" aria-label="刷新销售数据" onClick={refresh} disabled={busy}><RefreshCcw /></button>
        <button type="button" title="切换全屏" aria-label="切换全屏" onClick={() => void toggleFullscreen()}><Hexagon /><span className={styles.hexagonDot} /></button>
        <button type="button" title="帮助" aria-label="帮助" onClick={() => setSystemMessage('选择商场和时段可查看已推送的销售汇总，点击刷新获取最新数据。')}><CircleHelp /></button>
        <button type="button" title="退出登录" aria-label="退出登录" onClick={onLogout}><LogOut /></button>
      </div>
    </header>

    <nav className={styles.secondaryNav} aria-label="销售数据导航">
      <button type="button" className={styles.secondaryActive} aria-current="page" onClick={() => setSystemMessage('')}>概览</button>
      <button type="button" onClick={() => onNavigate?.('business-overview')}>付款明细</button>
      <button type="button" onClick={() => setSystemMessage('TOP20商品暂未接入。')}>TOP20商品</button>
      <button type="button" onClick={() => setSystemMessage('员工业绩暂未接入。')}>员工业绩</button>
    </nav>
    <div className={styles.divider} aria-hidden="true"><ChevronLeft /><ChevronRight /></div>

    <main className={styles.content}>
      <section className={styles.filters} aria-label="销售查询条件">
        <div className={styles.periodControls}>
          <strong>时段:</strong>
          <div className={styles.periodButtons}>
            {([['today', '今日'], ['week', '本周'], ['month', '本月']] as const).map(([key, label]) =>
              <button key={key} type="button" aria-pressed={period === key} className={period === key ? styles.periodActive : ''} onClick={() => selectPeriod(key)}>{label}</button>)}
          </div>
          <div className={styles.dateControl}>
            <button type="button" className={styles.dateRange} aria-expanded={datePickerOpen} onClick={() => { setDraftRange(range); setDateError(''); setDatePickerOpen(!datePickerOpen) }}>{range.startDate} 至 {range.endDate}</button>
            {datePickerOpen && <form className={styles.datePicker} onSubmit={(event) => { event.preventDefault(); applyDateRange() }}>
              <label>开始日期<input type="date" value={draftRange.startDate} max={draftRange.endDate || undefined} required onChange={(event) => setDraftRange((current) => ({ ...current, startDate: event.target.value }))} /></label>
              <label>结束日期<input type="date" value={draftRange.endDate} min={draftRange.startDate || undefined} required onChange={(event) => setDraftRange((current) => ({ ...current, endDate: event.target.value }))} /></label>
              {dateError && <p role="alert">{dateError}</p>}
              <div><button type="button" onClick={() => setDatePickerOpen(false)}>取消</button><button type="submit">确定</button></div>
            </form>}
          </div>
        </div>
        <button type="button" className={styles.refresh} onClick={refresh} disabled={busy}><RefreshCcw className={busy ? styles.spinning : ''} />{busy ? '加载中' : '刷新'}</button>
      </section>

      <section className={styles.salesPanel} aria-label="销售数据" aria-busy={busy}>
        <h1>销售数据</h1>
        <div className={styles.metrics}>
          {metrics.map((metric) => <div className={styles.metric} key={metric.key}>
            <div className={styles.metricLabel}>{metric.label}</div>
            <div className={styles.metricValue}>{summary ? formatMetric(summary.metrics[metric.key], metric.format) : busy ? '…' : '—'}</div>
          </div>)}
        </div>
      </section>
    </main>

    <footer className={styles.statusBar}>
      <div className={styles.systemMessage} role="status" aria-live="polite"><Volume2 aria-hidden="true" /><span>系统消息：</span><span className={styles.messageText} title={message}>{message}</span></div>
      <div className={styles.statusDetails}>
        <span>欢迎使用云</span>
        <span className={styles.weather} title={weather ? `${weather.date} 天级天气` : '当天暂无天级天气数据'}>{weatherText}{weather && <WeatherIcon skycon={weather.skycon} />}</span>
        <span className={styles.user} title={`${username}（${selectedMall?.nameCn ?? '未选择商场'}）`}>用户名: {username}（{selectedMall?.nameCn ?? '未选择商场'}）</span>
        <span className={styles.cloudStatus}>P4 <CloudUpload aria-hidden="true" /></span>
        <span className={online ? styles.network : styles.offline}>{online ? <Wifi aria-hidden="true" /> : <WifiOff aria-hidden="true" />}网络状态：{online ? '优' : '离线'}</span>
      </div>
    </footer>
  </div>
}
