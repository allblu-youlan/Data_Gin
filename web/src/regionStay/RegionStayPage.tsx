import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Activity, Download, RefreshCw } from 'lucide-react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { formatShanghaiDateTime } from '../dateTime'
import { MallWeatherChart } from '../MallWeatherChart'
import { parsePassengerMalls, passengerFilterError, passengerPreset, type PassengerMall } from '../passengerFlow/contracts'
import { Button, DataTable, FeedbackState, FilterToolbar, MetricStrip, PageCanvas, PageHeader, PaginationControls, Section } from '../ui'
import { parseRegionOverview, regionDailyChart, regionFilterBody, regionGroups, regionKey, regionQueryPath, saveRegionWorkbook, type RegionFilter, type RegionOverview } from './contracts'
import { RegionSyncPanel } from './RegionSyncPanel'
import styles from './RegionStayPage.module.css'

const number = new Intl.NumberFormat('zh-CN')
const ratio = (value: number) => `${(value * 100).toFixed(2)}%`
const initialFilter = (): RegionFilter => ({ ...passengerPreset(7), regionName: '' })

export function RegionStayPage({ client, canManage = false }: { client: WorkspaceApiClient; canManage?: boolean }) {
  const [filter, setFilter] = useState<RegionFilter>(initialFilter)
  const [malls, setMalls] = useState<PassengerMall[]>([])
  const [mallError, setMallError] = useState('')
  const [snapshot, setSnapshot] = useState<{ filter: RegionFilter; data: RegionOverview } | null>(null)
  const [regionOptions, setRegionOptions] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [exporting, setExporting] = useState(false)
  const [downloadMessage, setDownloadMessage] = useState('')
  const [downloadError, setDownloadError] = useState('')
  const [selectedKey, setSelectedKey] = useState('')
  const [page, setPage] = useState(1)
  const queryController = useRef<AbortController | null>(null)
  const optionsController = useRef<AbortController | null>(null)
  const exportController = useRef<AbortController | null>(null)

  const loadMalls = useCallback(async () => {
    optionsController.current?.abort()
    const controller = new AbortController(); optionsController.current = controller
    setMallError('')
    try {
      const response = await client('/v1/kaiyu-passenger/malls', { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true })
      if (controller.signal.aborted) return
      const parsed = response.ok ? parsePassengerMalls(response.data) : null
      if (!parsed) { setMallError('门店选项加载失败，仍可查询全部授权门店。'); return }
      setMalls(parsed)
    } catch { if (!controller.signal.aborted) setMallError('门店选项加载失败，请重试。') }
  }, [client])

  const load = useCallback(async (selected: RegionFilter) => {
    queryController.current?.abort()
    const validation = passengerFilterError(selected)
    if (validation) { setSnapshot(null); setLoading(false); setError(validation); return }
    const controller = new AbortController(); queryController.current = controller
    setLoading(true); setSnapshot(null); setError(''); setDownloadMessage(''); setDownloadError('')
    try {
      const response = await client(regionQueryPath(selected), { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 60_000 })
      if (controller.signal.aborted) return
      if (!response.ok) { setError(response.error?.message || '区域数据查询失败，请重试。'); return }
      const data = parseRegionOverview(response.data)
      if (!data) { setError('区域数据格式异常，请重新查询。'); return }
      setSnapshot({ filter: { ...selected }, data }); setPage(1)
      if (!selected.regionName) setRegionOptions([...new Set(data.items.map(item => item.regionName))].sort())
    } catch { if (!controller.signal.aborted) setError('区域数据查询失败，请检查网络后重试。') }
    finally { if (!controller.signal.aborted) setLoading(false) }
  }, [client])

  useEffect(() => {
    void load(initialFilter()); void loadMalls()
    return () => { queryController.current?.abort(); optionsController.current?.abort(); exportController.current?.abort() }
  }, [load, loadMalls])

  const changed = Boolean(snapshot && Object.keys(filter).some(key => filter[key as keyof RegionFilter] !== snapshot.filter[key as keyof RegionFilter]))
  const data = snapshot?.data
  const canDownload = Boolean(snapshot && !changed && !loading && !exporting)
  const groups = useMemo(() => regionGroups(data?.items || []), [data])
  const selected = groups.find(group => group.key === selectedKey) || groups[0]
  const maxStay = Math.max(1, ...groups.map(group => group.stayCount))
  const series = useMemo(() => snapshot ? [
    { id: 'exposure', name: '曝光人次', color: 'var(--color-chart-amber)', dash: '6 4', data: regionDailyChart(snapshot.data.items, snapshot.filter, 'exposureCount') },
    { id: 'stay', name: '停留人次', color: 'var(--color-chart-emerald)', data: regionDailyChart(snapshot.data.items, snapshot.filter, 'stayCount') },
  ] : [], [snapshot])
  const durationSeries = useMemo(() => snapshot && selected ? [{ id: 'duration', name: '平均停留时长', color: 'var(--color-chart-teal)',
    data: regionDailyChart(snapshot.data.items.filter(item => regionKey(item) === selected.key), snapshot.filter, 'avgStayTime'),
  }] : [], [snapshot, selected])
  const rateSeries = useMemo(() => snapshot && selected ? [{ id: 'rate', name: '停留率', color: 'var(--color-chart-emerald)',
    data: regionDailyChart(snapshot.data.items.filter(item => regionKey(item) === selected.key), snapshot.filter, 'stayRate'),
  }] : [], [snapshot, selected])

  async function download() {
    if (!snapshot || !canDownload) return
    const controller = new AbortController(); exportController.current = controller
    setExporting(true); setDownloadMessage(''); setDownloadError('')
    try {
      const response = await client('/v1/kaiyu-region-stay/export', { method: 'POST', body: regionFilterBody(snapshot.filter), responseType: 'blob', signal: controller.signal,
        showResult: false, silentLoading: true, acceptSafeErrorMessage: true, timeoutMs: 120_000, retry: false })
      if (controller.signal.aborted) return
      if (!response.ok) { setDownloadError(response.error?.message || 'XLSX 下载失败，请稍后重试。'); return }
      if (!(response.data instanceof Blob)) { setDownloadError('下载内容异常，请重试。'); return }
      saveRegionWorkbook(response.data, snapshot.filter)
      setDownloadMessage('XLSX 已开始下载，包含区域每日明细和门店日期同步状态。')
    } catch (reason) { if (!controller.signal.aborted) setDownloadError(reason instanceof Error ? reason.message : 'XLSX 下载失败，请重试。') }
    finally { if (!controller.signal.aborted) setExporting(false) }
  }
  function submit(event: FormEvent) { event.preventDefault(); void load(filter) }
  function selectMall(mallId: string) {
    const next = { ...filter, mallId, regionName: '' }
    setFilter(next); setRegionOptions([]); setSelectedKey('')
    void load(next)
  }
  function preset(days: number) {
    const next = { ...filter, ...passengerPreset(days), mallId: filter.mallId }
    setFilter(next); void load(next)
  }

  return <PageCanvas className={styles.page}>
    <PageHeader title="店内区域" description="根据已绑定的开域店铺 ID 自动采集区域名称及每日停留指标。" actions={
      <Button variant="primary" disabled={!canDownload} onClick={() => void download()}><Download aria-hidden="true" />{exporting ? '正在生成 XLSX…' : '下载 XLSX'}</Button>
    } />
    <form onSubmit={submit}>
      <FilterToolbar summary="上海时间 · 按日统计 · 最多 31 天" actions={<><Button type="submit" variant="primary" disabled={loading}>查询</Button><Button disabled={loading} aria-label="刷新区域数据" onClick={() => void load(filter)}><RefreshCw aria-hidden="true" /></Button></>}>
        <label>门店<select value={filter.mallId} onChange={event => selectMall(event.target.value)}><option value="">全部授权门店</option>{malls.map(mall => <option key={mall.mallId} value={mall.mallId}>{mall.mallName}{!mall.shopId ? '（未绑定）' : ''}</option>)}</select></label>
        <label>开始日期<input type="date" required value={filter.startDate} onChange={event => setFilter({ ...filter, startDate: event.target.value })} /></label>
        <label>结束日期<input type="date" required value={filter.endDate} onChange={event => setFilter({ ...filter, endDate: event.target.value })} /></label>
        <label>区域<select value={filter.regionName} disabled={loading} onChange={event => setFilter({ ...filter, regionName: event.target.value })}><option value="">全部区域</option>{[...new Set([...regionOptions, ...(filter.regionName ? [filter.regionName] : [])])].map(name => <option key={name} value={name}>{name}</option>)}</select></label>
        <div className={styles.presets}><Button onClick={() => preset(1)}>今天</Button><Button onClick={() => preset(7)}>近 7 天</Button><Button onClick={() => preset(30)}>近 30 天</Button></div>
      </FilterToolbar>
    </form>
    {mallError && <div className={styles.notice} role="status">{mallError}<Button onClick={() => void loadMalls()}>重试门店列表</Button></div>}
    {changed && <p className={styles.notice} role="status">筛选条件已更改，点击“查询”后更新图表和下载范围。</p>}
    {error && <FeedbackState kind="error" title="无法加载区域数据" description={error} action={<Button onClick={() => void load(filter)}>重试</Button>} />}
    {downloadError && <p className={styles.error} role="alert">{downloadError}</p>}
    {downloadMessage && <p className={styles.notice} role="status">{downloadMessage}</p>}
    {loading ? <FeedbackState kind="loading" title="正在加载区域数据" description="正在查询所选门店和日期范围内的区域记录。" /> : data && <>
      <div className={styles.context}><span>{data.startDate} 至 {data.endDate} · {snapshot?.filter.mallId ? malls.find(mall => String(mall.mallId) === snapshot.filter.mallId)?.mallName || '所选门店' : '全部授权门店'} · {snapshot?.filter.regionName || '全部区域'}</span><span>最近采集：{formatShanghaiDateTime(data.summary.latestFetchedAt)}</span></div>
      <MetricStrip label="区域数据汇总" items={[
        { key: 'exposure', label: '曝光人次', value: data.summary.recordCount ? number.format(data.summary.exposureCount) : '—', detail: '所选区域逐日累计，未去重' },
        { key: 'stay', label: '停留人次', value: data.summary.recordCount ? number.format(data.summary.stayCount) : '—', detail: '所选区域逐日累计，未去重' },
        { key: 'rate', label: '汇总停留率', value: data.summary.exposureCount ? ratio(data.summary.stayCount / data.summary.exposureCount) : '—', detail: '总停留人次 ÷ 总曝光人次' },
        { key: 'regions', label: '有数据区域', value: number.format(data.summary.regionCount), detail: `${data.summary.mallCount} 家门店 · ${data.summary.recordCount} 条日记录` },
      ]} />
      {!data.items.length ? <FeedbackState kind="empty" title="所选范围暂无区域数据" description="在“店铺信息”填写开域店铺 ID 后即可采集；可在下方检查同步状态并补拉。源端未返回数据不表示零客流。" icon={<Activity aria-hidden="true" />} /> : <>
        <div className={styles.charts}>
          <div className={styles.trends}>
            <MallWeatherChart title="每日曝光与停留" detail="所选区域有记录数据按日合计 · 人次未去重" unit="人次" series={series} floorZero onDownload={() => void download()} downloadDisabled={!canDownload} />
            {selected && <>
              <MallWeatherChart title="区域平均停留时长" detail={`${selected.mallName} · ${selected.regionName} · 接口单日原始值`} unit="秒" series={durationSeries} floorZero onDownload={() => void download()} downloadDisabled={!canDownload} />
              <MallWeatherChart title="区域每日停留率" detail={`${selected.mallName} · ${selected.regionName} · 停留人次 ÷ 曝光人次`} unit="%" series={rateSeries} floorZero onDownload={() => void download()} downloadDisabled={!canDownload} />
            </>}
          </div>
          <Section title="区域停留对比" description="按累计停留人次排序，点击区域查看时长及停留率。">
            <div className={styles.ranking} aria-label="区域停留人次对比">{groups.map(group => <button type="button" key={group.key} aria-pressed={selected?.key === group.key} onClick={() => setSelectedKey(group.key)} aria-label={`查看${group.mallName}的${group.regionName}，停留 ${group.stayCount} 人次`}>
              <span className={styles.barTitle}><strong>{group.regionName}</strong><b>{number.format(group.stayCount)}</b></span>
              <span className={styles.meta}>{group.mallName} · {group.recordCount} 天有记录</span>
              <span className={styles.track} aria-hidden="true"><span style={{ width: `${group.stayCount / maxStay * 100}%` }} /></span>
              <span className={styles.meta}>曝光 {number.format(group.exposureCount)} · 停留率 {group.stayRate === undefined ? '—' : ratio(group.stayRate)}</span>
            </button>)}</div>
          </Section>
        </div>
        <p className={styles.footnote}>曲线空白表示未采集到记录，不按零计算；各日覆盖的区域可能不同。人次未跨区域、跨日去重，不能视为门店独立人数。平均停留时长仅展示单区域单日原始值；今天的数据仍可能更新。</p>
        <Section title="区域每日明细" description="保留源端停留率和平均停留秒数，全部明细可下载 XLSX。">
          <DataTable minWidth={1020} density="compact" scrollLabel="区域每日监控明细">
            <thead><tr><th scope="col">门店</th><th scope="col">区域</th><th scope="col">日期</th><th scope="col">曝光人次</th><th scope="col">停留人次</th><th scope="col">停留率</th><th scope="col">平均停留（秒）</th><th scope="col">采集时间（上海）</th></tr></thead>
            <tbody>{data.items.slice((page - 1) * 50, page * 50).map(item => <tr key={`${regionKey(item)}:${item.statDate}`}>
              <td>{item.mallName}</td><td>{item.regionName}</td><td>{item.statDate}</td><td className={styles.tableNumber}>{number.format(item.exposureCount)}</td><td className={styles.tableNumber}>{number.format(item.stayCount)}</td><td className={styles.tableNumber}>{ratio(item.stayRate)}</td><td className={styles.tableNumber}>{item.avgStayTime.toFixed(2)}</td><td>{formatShanghaiDateTime(item.fetchedAt)}</td>
            </tr>)}</tbody>
          </DataTable>
          <PaginationControls page={page} totalPages={Math.ceil(data.items.length / 50)} onPrevious={() => setPage(p => p - 1)} onNext={() => setPage(p => p + 1)} />
        </Section>
      </>}
    </>}
    <RegionSyncPanel key={`${filter.startDate}:${filter.endDate}:${filter.mallId}:${filter.regionName}`} client={client} filter={filter} canManage={canManage} disabled={changed || loading || Boolean(passengerFilterError(filter))} onUpdated={() => void load(filter)} />
  </PageCanvas>
}
