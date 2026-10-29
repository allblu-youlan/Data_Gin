import { formatShanghaiDateTime } from '../../dateTime.js'
import { Activity, AlertTriangle, Server } from 'lucide-react'
import type { DataStatisticsSummary, HealthSummary, MallWeatherMetricsSummary } from '../../monitoring'
import { DataTable, FeedbackState, MetricStrip, PageCanvas, PageHeader, Section, StatusTag, type StatusTagTone } from '../../ui'
import styles from './RunOverviewPage.module.css'
import type { PipelineRun } from '../types'

export type OverviewPipelineRun = Pick<PipelineRun, 'id' | 'trace_id' | 'run_type' | 'trigger_type' | 'status' | 'total_count' | 'success_count' | 'failed_count' | 'started_at'>

export interface OverviewDeliveryLog {
  id: number
  destination_name: string
  destination_code: string
  destination_id: number
  http_status: number
  success: boolean
  sent_at: string | null
}

export interface RunOverviewPageProps {
  runs: OverviewPipelineRun[]
  deliveryLogs: OverviewDeliveryLog[]
  monitoring: {
    statistics: DataStatisticsSummary | null
    weather: MallWeatherMetricsSummary | null
    health: HealthSummary | null
  }
  stale: boolean
  overviewTotals: { runs: number | null; deliveryLogs: number | null }
  onLoadSteps: (runId: number) => void
}

export function RunOverviewPage({ runs, deliveryLogs, monitoring, stale, overviewTotals, onLoadSteps }: RunOverviewPageProps) {
  const failedLogs = deliveryLogs.filter((log) => !log.success)
  const runningRuns = runs.filter((run) => run.status === 'running')
  const loadedRunTotal = sum(runs, 'success_count') + sum(runs, 'failed_count')
  const successRate = loadedRunTotal > 0 ? sum(runs, 'success_count') / loadedRunTotal : null
  const delivered = deliveryLogs.filter((log) => log.success).length
  const deliveryRate = deliveryLogs.length > 0 ? delivered / deliveryLogs.length : null
  const healthTotal = delivered + failedLogs.length

  return (
    <PageCanvas className={styles.page}>
      <PageHeader
        eyebrow="OPERATIONS"
        title="运行总览"
        description="集中查看今日流水线进度、交付健康度与最近异常。"
        context={<StatusTag tone={monitoring.health?.healthy ? 'success' : 'neutral'}>{monitoring.health?.healthy ? '服务正常' : '状态未知'}</StatusTag>}
      />
      <MetricStrip label="今日运行关键指标" items={[
        { key: 'runs', label: '今日运行', value: overviewTotals.runs ?? runs.length, detail: `已加载 ${runs.length} 条` },
        { key: 'rate', label: '运行成功率', value: successRate === null ? '-' : `${(successRate * 100).toFixed(1)}%`, detail: '按已加载处理量' },
        { key: 'running', label: '待处理运行', value: runningRuns.length, detail: '当前运行中' },
        { key: 'failed', label: '失败交付', value: overviewTotals.deliveryLogs === null ? failedLogs.length : `${failedLogs.length} / ${overviewTotals.deliveryLogs}`, detail: '失败 / 今日总量' },
      ]} />
      {stale ? <div className={styles.stale} role="status">部分统计暂时不可用，已保留最近一次成功数据。</div> : null}
      <div className={styles.workspace}>
        <Section className={styles.runs} title="最近流水线运行" description={`今日已加载 ${runs.length} 条，最多展示最近 12 条。`} flush>
          <OverviewRunTable runs={runs} onLoadSteps={onLoadSteps} />
        </Section>
        <aside className={styles.monitoring} aria-label="交付健康度与最近异常">
          <section className={styles.monitoringSection}>
            <div className={styles.sectionHeading}><Activity aria-hidden="true" /><h2>交付健康度</h2></div>
            <progress className={styles.healthProgress} value={healthTotal ? delivered : 0} max={Math.max(healthTotal, 1)} aria-label="已加载交付成功率" />
            <div className={styles.healthLegend}><span className={styles.success}>推送成功 {delivered}</span><span className={styles.danger}>推送失败 {failedLogs.length}</span></div>
            <small>{deliveryRate === null ? '今日暂无已加载交付记录。' : `已加载交付成功率 ${(deliveryRate * 100).toFixed(1)}%`}</small>
          </section>
          <section className={styles.monitoringSection}>
            <div className={styles.sectionHeading}><AlertTriangle aria-hidden="true" /><h2>最近异常</h2></div>
            {failedLogs.length === 0 && monitoring.weather?.firingAlerts === 0
              ? <FeedbackState className={styles.compactFeedback} kind="empty" title="暂无已加载异常" />
              : <div className={styles.anomalyList}>
                {failedLogs.slice(0, 4).map((log) => <article className={styles.anomaly} key={log.id}><strong>{log.destination_name || log.destination_code || `目标 #${log.destination_id}`} 推送失败</strong><span>{formatDate(log.sent_at)} / HTTP {log.http_status || '-'}</span></article>)}
                {(monitoring.weather?.firingAlerts ?? 0) > 0 ? <article className={styles.anomaly}><strong>天气服务告警</strong><span>当前触发 {monitoring.weather?.firingAlerts} 条告警</span></article> : null}
              </div>}
          </section>
          <section className={styles.monitoringSection}>
            <div className={styles.sectionHeading}><Server aria-hidden="true" /><h2>服务摘要</h2></div>
            <strong className={monitoring.health?.healthy ? styles.success : styles.muted}>{monitoring.health?.healthy ? '系统正常' : '状态未知'}</strong>
            <small>接收 {monitoring.statistics?.totalCount ?? '-'} / 已处理 {monitoring.statistics?.processedCount ?? '-'} / 处理失败 {monitoring.statistics?.errorCount ?? '-'}</small>
          </section>
        </aside>
      </div>
    </PageCanvas>
  )
}

function OverviewRunTable({ runs, onLoadSteps }: { runs: OverviewPipelineRun[]; onLoadSteps: (runId: number) => void }) {
  if (runs.length === 0) return <FeedbackState kind="empty" title="今日暂无运行记录" />
  return <DataTable containerClassName={styles.table} minWidth={780} scrollLabel="最近流水线运行记录"><thead><tr><th scope="col">运行任务</th><th scope="col">来源</th><th scope="col">状态</th><th scope="col">处理进度</th><th scope="col">开始时间</th><th scope="col">操作</th></tr></thead><tbody>{runs.slice(0, 12).map((run) => {
    const completed = run.success_count + run.failed_count
    const progress = run.total_count > 0 ? Math.min(100, Math.round(completed / run.total_count * 100)) : run.status === 'success' ? 100 : 0
    return <tr key={run.id}><td><strong>#{run.id} {run.run_type}</strong><small>{run.trace_id || '-'}</small></td><td>{run.trigger_type || '-'}</td><td><StatusTag tone={runStatusTone(run.status)}>{runStatusLabel(run.status)}</StatusTag></td><td><div className={styles.runProgress}><span>{completed} / {run.total_count} ({progress}%)</span><progress value={progress} max="100" aria-label={`运行 #${run.id} 处理进度`} /></div></td><td>{formatDate(run.started_at)}</td><td><button type="button" onClick={() => onLoadSteps(run.id)}>查看步骤</button></td></tr>
  })}</tbody></DataTable>
}

function runStatusTone(status: string): StatusTagTone {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'running'
  if (status === 'partial_success') return 'warning'
  return 'neutral'
}

function runStatusLabel(status: string) {
  const labels: Record<string, string> = { success: '已完成', running: '运行中', failed: '失败', partial_success: '部分成功' }
  return labels[status] ?? (status || '未知')
}

function formatDate(value: string | null) {
  return formatShanghaiDateTime(value, '-')
}

function sum(items: OverviewPipelineRun[], key: 'success_count' | 'failed_count') {
  return items.reduce((total, item) => total + (Number(item[key]) || 0), 0)
}
