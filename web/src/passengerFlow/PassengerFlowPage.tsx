import { formatShanghaiDateTime } from '../dateTime.js'
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Activity, Download, RefreshCw } from 'lucide-react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { MallWeatherChart } from '../MallWeatherChart'
import { PassengerBackfillPanel } from './PassengerBackfillPanel'
import { Button, FeedbackState, FilterToolbar, MetricStrip, PageCanvas, PageHeader, Section } from '../ui'
import {
  parsePassengerMalls, parsePassengerOverview, passengerChartData, passengerFilterBody, passengerFilterError,
  passengerPreset, passengerQueryPath, savePassengerWorkbook,
  type PassengerFilter, type PassengerMall, type PassengerOverview,
} from './contracts'
import styles from './PassengerFlowPage.module.css'

const number = new Intl.NumberFormat('zh-CN')
const ratio = (value: number) => `${(value * 100).toFixed(1)}%`
const localHour = formatShanghaiDateTime

export function PassengerFlowPage({ client, canManage = false }: { client: WorkspaceApiClient; canManage?: boolean }) {
  const [filter, setFilter] = useState<PassengerFilter>(() => passengerPreset(1))
  const [malls, setMalls] = useState<PassengerMall[]>([])
  const [mallError, setMallError] = useState('')
  const [snapshot, setSnapshot] = useState<{ filter: PassengerFilter; data: PassengerOverview } | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [exporting, setExporting] = useState(false)
  const [downloadMessage, setDownloadMessage] = useState('')
  const [downloadError, setDownloadError] = useState('')
  const queryController = useRef<AbortController | null>(null)
  const optionsController = useRef<AbortController | null>(null)
  const exportController = useRef<AbortController | null>(null)

  const loadMalls = useCallback(async () => {
    optionsController.current?.abort()
    const controller = new AbortController()
    optionsController.current = controller
    setMallError('')
    try {
      const response = await client('/v1/kaiyu-passenger/malls', { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true })
      if (controller.signal.aborted) return
      const parsed = response.ok ? parsePassengerMalls(response.data) : null
      if (!parsed) { setMallError('门店选项加载失败，仍可查询全部授权门店。'); return }
      setMalls(parsed)
    } catch {
      if (!controller.signal.aborted) setMallError('门店选项加载失败，请重试。')
    }
  }, [client])

  const load = useCallback(async (selected: PassengerFilter) => {
    const validation = passengerFilterError(selected)
    if (validation) { setError(validation); return }
    queryController.current?.abort()
    const controller = new AbortController()
    queryController.current = controller
    setLoading(true)
    setSnapshot(null)
    setError('')
    setDownloadMessage('')
    setDownloadError('')
    try {
      const response = await client(passengerQueryPath(selected), {
        method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 60_000,
      })
      if (controller.signal.aborted) return
      if (!response.ok) { setError(response.error?.message || '客流查询失败，请稍后重试。'); return }
      const data = parsePassengerOverview(response.data)
      if (!data) { setError('客流数据格式异常，请重新查询。'); return }
      setSnapshot({ filter: { ...selected }, data })
    } catch {
      if (!controller.signal.aborted) setError('客流查询失败，请检查网络后重试。')
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [client])

  useEffect(() => {
    void load(passengerPreset(1))
    void loadMalls()
    return () => {
      queryController.current?.abort()
      optionsController.current?.abort()
      exportController.current?.abort()
    }
  }, [load, loadMalls])

  const changed = Boolean(snapshot && (snapshot.filter.startDate !== filter.startDate || snapshot.filter.endDate !== filter.endDate || snapshot.filter.mallId !== filter.mallId))
  const data = snapshot?.data
  const canDownload = Boolean(data?.summary.recordCount && !changed && !loading && !exporting)
  const chartSeries = useMemo(() => data ? [
    { id: 'traffic', name: '入店人次', color: 'var(--color-chart-emerald)', data: passengerChartData(data.trend, 'traffic') },
    { id: 'passby', name: '客流量', color: 'var(--color-chart-amber)', dash: '6 4', data: passengerChartData(data.trend, 'passby') },
  ] : [], [data])
  const rateSeries = useMemo(() => data ? [
    { id: 'captureRate', name: '入店率', color: 'var(--color-chart-teal)', data: passengerChartData(data.trend, 'captureRate') },
  ] : [], [data])
  const rankedMalls = useMemo(() => [...(data?.malls || [])].sort((a, b) => b.traffic - a.traffic || a.mallId - b.mallId), [data])
  const maxTraffic = Math.max(1, ...rankedMalls.map(mall => mall.traffic))

  async function download() {
    if (!snapshot || !canDownload) return
    const controller = new AbortController()
    exportController.current = controller
    setExporting(true)
    setDownloadMessage('')
    setDownloadError('')
    try {
      const response = await client('/v1/kaiyu-passenger/export', {
        method: 'POST', body: passengerFilterBody(snapshot.filter), responseType: 'blob', signal: controller.signal,
        showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 120_000, retry: false,
      })
      if (controller.signal.aborted) return
      if (!response.ok) { setDownloadError(response.error?.message || 'XLSX 下载失败，请稍后重试。'); return }
      if (!(response.data instanceof Blob)) { setDownloadError('下载内容异常，请重试。'); return }
      savePassengerWorkbook(response.data, snapshot.filter)
      setDownloadMessage('XLSX 已生成并开始下载，包含门店汇总、小时趋势和小时明细。')
    } catch (reason) {
      if (!controller.signal.aborted) setDownloadError(reason instanceof Error ? reason.message : 'XLSX 下载失败，请稍后重试。')
    } finally {
      if (!controller.signal.aborted) setExporting(false)
    }
  }

  function submit(event: FormEvent) { event.preventDefault(); void load(filter) }
  function preset(days: number) {
    const selected = { ...passengerPreset(days), mallId: filter.mallId }
    setFilter(selected)
    void load(selected)
  }
  function selectMall(mallId: number) {
    if (!snapshot) return
    const selected = { ...snapshot.filter, mallId: String(mallId) }
    setFilter(selected)
    void load(selected)
  }

  return <PageCanvas className={styles.page}>
    <PageHeader title="监控客流" description="按小时查看门店客流与入店表现，下载筛选范围内的 XLSX 明细。" actions={
      <Button variant="primary" disabled={!canDownload} onClick={() => void download()}><Download aria-hidden="true" />{exporting ? '正在生成 XLSX…' : '下载 XLSX'}</Button>
    } />
    <form onSubmit={submit}>
      <FilterToolbar actions={<><Button type="submit" variant="primary" disabled={loading}>查询</Button><Button disabled={loading} aria-label="刷新客流数据" onClick={() => void load(filter)}><RefreshCw aria-hidden="true" /></Button></>} summary="上海时间 · 最多 31 天">
        <label>门店<select value={filter.mallId} onChange={event => setFilter({ ...filter, mallId: event.target.value })}><option value="">全部授权门店</option>{malls.map(mall => <option key={mall.mallId} value={mall.mallId}>{mall.mallName}{!mall.shopId ? '（未绑定）' : ''}</option>)}</select></label>
        <label>开始日期<input type="date" required value={filter.startDate} onChange={event => setFilter({ ...filter, startDate: event.target.value })} /></label>
        <label>结束日期<input type="date" required value={filter.endDate} onChange={event => setFilter({ ...filter, endDate: event.target.value })} /></label>
        <div className={styles.presets}><Button onClick={() => preset(1)}>今天</Button><Button onClick={() => preset(7)}>近 7 天</Button><Button onClick={() => preset(30)}>近 30 天</Button></div>
      </FilterToolbar>
    </form>
    {mallError && <div className={styles.notice} role="status">{mallError}<Button onClick={() => void loadMalls()}>重试门店列表</Button></div>}
    {changed && <div className={styles.notice} role="status">筛选条件已更改，点击“查询”后更新图表和下载范围。</div>}
    {error && <FeedbackState kind="error" title="无法加载客流" description={error} action={<Button onClick={() => void load(filter)}>重试</Button>} />}
    {downloadError && <p className={styles.error} role="alert">{downloadError}</p>}
    {downloadMessage && <p className={styles.notice} role="status">{downloadMessage}</p>}
    <PassengerBackfillPanel key={`${filter.startDate}:${filter.endDate}:${filter.mallId}`} client={client} filter={filter} canManage={canManage} disabled={changed || loading || Boolean(passengerFilterError(filter))} onUpdated={() => void load(filter)} />
    {loading ? <FeedbackState kind="loading" title="正在加载客流数据" description="正在汇总所选门店和日期范围内的小时记录。" /> : data && <>
      <div className={styles.context}><span>{data.startDate} 至 {data.endDate} · {snapshot?.filter.mallId ? malls.find(mall => String(mall.mallId) === snapshot.filter.mallId)?.mallName || '所选门店' : '全部授权门店'}</span><span>最新数据小时：{data.summary.latestHour ? localHour(data.summary.latestHour) : '暂无'}</span></div>
      <MetricStrip label="监控客流汇总" items={[
        { key: 'traffic', label: '入店人次', value: data.summary.recordCount ? number.format(data.summary.traffic) : '—', detail: '所选时段逐小时累计' },
        { key: 'passby', label: '客流量', value: data.summary.recordCount ? number.format(data.summary.passby) : '—', detail: '所选时段逐小时累计' },
        { key: 'rate', label: '入店率', value: data.summary.passby > 0 ? ratio(data.summary.captureRate) : '—', detail: '入店人次 ÷ 客流量' },
        { key: 'malls', label: '有数据门店', value: number.format(data.summary.mallCount), detail: `${number.format(data.summary.recordCount)} 条小时记录` },
      ]} />
      {!data.summary.recordCount ? <FeedbackState kind="empty" title="所选范围暂无客流数据" description="请在“店铺信息”填写开域店铺 ID，并等待每小时同步完成；也可切换门店或扩大日期范围。" icon={<Activity aria-hidden="true" />} /> : <>
        <div className={styles.charts}>
          <div className={styles.trends}>
            <MallWeatherChart title="小时客流趋势" detail="所有有记录门店按小时合计" unit="人次" series={chartSeries} floorZero onDownload={() => void download()} downloadDisabled={!canDownload} />
            <MallWeatherChart title="小时入店率" detail="各小时入店人次 ÷ 客流量" unit="%" series={rateSeries} floorZero onDownload={() => void download()} downloadDisabled={!canDownload} />
          </div>
          <Section title="门店客流对比" description="按入店人次排序，点击门店查看趋势。">
            <div className={styles.ranking} aria-label="门店入店人次对比">
              {rankedMalls.map(mall => <button className={styles.mallBar} key={`${mall.mallId}:${mall.shopId}`} onClick={() => selectMall(mall.mallId)} aria-label={`查看${mall.mallName}：入店人次 ${mall.traffic}`}>
                <span className={styles.barTitle}><strong>{mall.mallName}</strong><b>{number.format(mall.traffic)}</b></span>
                <span className={styles.track} aria-hidden="true"><span style={{ width: `${mall.traffic / maxTraffic * 100}%` }} /></span>
                <span className={styles.barMeta}>客流量 {number.format(mall.passby)} · 入店率 {mall.passby ? ratio(mall.captureRate) : '—'}</span>
              </button>)}
            </div>
          </Section>
        </div>
        <p className={styles.footnote}>曲线空白表示该小时没有采集记录，不作为零客流。不同小时可能覆盖不同门店，数据会随每小时同步补充。入店人数为小时累计，未跨小时去重；XLSX 保留全部原始指标。</p>
      </>}
    </>}
  </PageCanvas>
}
