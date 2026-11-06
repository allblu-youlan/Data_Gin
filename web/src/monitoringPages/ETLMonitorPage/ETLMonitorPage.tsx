import { useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { Button, FeedbackState, PageCanvas, PageHeader, StatusTag } from '../../ui'
import type { MonitoringClient } from '../types'
import { formatMonitoringDate } from '../contracts'
import { parseETLMonitor, type ETLMonitorSnapshot } from './contracts'
import { ETLMeasurements, ETLProbes } from './Measurements'
import { ETLHistory } from './History'
import { ETLSeriesTable } from './SeriesTable'
import styles from './ETLMonitorPage.module.css'

export function ETLMonitorPage({ client, refreshVersion }: { client: MonitoringClient; refreshVersion: number }) {
  const [snapshot, setSnapshot] = useState<ETLMonitorSnapshot | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [refresh, setRefresh] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    async function load() {
      setLoading(true)
      try {
        const response = await client('/v1/etl-monitor', { method: 'GET', signal: controller.signal, showResult: false, silentLoading: true })
        if (controller.signal.aborted) return
        const parsed = response.ok ? parseETLMonitor(response.data) : null
        if (parsed) {
          setSnapshot(parsed)
          setError('')
        } else {
          if (response.status === 401 || response.status === 403) setSnapshot(null)
          setError(response.error?.message || '监控数据返回格式无效，请稍后重试。')
        }
      } catch {
        if (!controller.signal.aborted) setError('监控数据暂时不可用，请检查连接或稍后重试。')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          timer = setTimeout(() => void load(), 15_000)
        }
      }
    }
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [client, refreshVersion, refresh])

  return <PageCanvas className={styles.page}>
    <PageHeader eyebrow="OPERATIONS / BENTO" title="ETL 监控"
      description="查看采集服务的存活、组件就绪、消息处理与运行时指标。"
      actions={<Button disabled={loading} onClick={() => setRefresh((value) => value + 1)}><RefreshCw size={14} aria-hidden="true" />{loading ? '读取中…' : '刷新快照'}</Button>}
      context={<StatusTag tone={snapshot?.configured ? 'info' : 'neutral'}>{snapshot?.configured ? '每 15 秒刷新' : '等待接入'}</StatusTag>} />
    {error ? <FeedbackState kind="error" title="监控快照读取失败" description={`${error}${snapshot ? ' 以下保留最近一次快照，不能代表当前状态。' : ''}`} /> : null}
    {!snapshot && loading ? <FeedbackState kind="loading" title="正在读取 ETL 监控" /> : null}
    {snapshot && !snapshot.configured ? <FeedbackState kind="empty" title={snapshot.config_error ? 'ETL 监控配置无效' : '尚未配置 ETL 监控地址'}
      description={snapshot.config_error || '请为监控服务配置 ETL_MONITOR_BASE_URL，填入从当前部署环境可访问的 ETL HTTP 基础地址；可包含代理路径前缀。保存并重启服务后开始采集。'} /> : null}
    {snapshot?.configured ? <>
      <div className={styles.metadata}><span>监控目标 <strong>{snapshot.target}</strong></span><span>最近指标采集 {formatMonitoringDate(snapshot.last_metrics_at)}</span></div>
      <div className={styles.notice}>就绪表示 Bento 输入和输出已连接，不代表每个任务最近同步成功。消息数可能包含整页数据，不能直接作为订单数。</div>
      {snapshot.stale ? <div className={styles.warning} role="status">{snapshot.last_metrics_at ? '指标采集失败或数据已过期，以下指标为最近一次成功采集结果。' : '尚未取得有效指标，正在等待首次采集。'}</div> : null}
      <ETLProbes snapshot={snapshot} />
      <ETLMeasurements summary={snapshot.summary} stale={snapshot.stale || !!error} />
      <ETLHistory points={snapshot.history} />
      <ETLSeriesTable series={snapshot.series} />
    </> : null}
  </PageCanvas>
}
