import { DataTable, FeedbackState, Section } from '../../ui'
import { formatMonitoringDate } from '../contracts'
import { formatETLValue, type ETLHistoryPoint } from './contracts'
import styles from './ETLMonitorPage.module.css'

const lines = [{ key: 'input_rate' as const, label: '输入消息 / 秒', className: styles.inputLine }, { key: 'output_rate' as const, label: '输出消息 / 秒', className: styles.outputLine }]
export function ETLHistory({ points }: { points: ETLHistoryPoint[] }) {
  const values = points.flatMap((point) => [point.input_rate, point.output_rate]).filter((value): value is number => value !== null)
  const max = Math.max(1, ...values)
  return <Section title="近期采集趋势" description="保留当前监控服务进程内最近 60 次采样；重启后重新积累。空缺速率不按零填充。" flush>
    {points.length < 2 || values.length === 0 ? <FeedbackState kind="empty" title="等待更多采样以显示趋势" description="通常需要约两个采集周期；暂无流量时可能尚未产生消息指标。" /> : <div className={styles.chart}>
      <div className={styles.legend}>{lines.map((line) => <span className={line.className} key={line.key}>{line.label}</span>)}<span>纵轴上限 {formatETLValue(max)}</span></div>
      <svg viewBox="0 0 760 180" role="img" aria-label="ETL 输入和输出消息每秒速率趋势，可在下方展开精确采样值" preserveAspectRatio="none">
        <line x1="0" y1="168" x2="760" y2="168" className={styles.axis} />
        <line x1="0" y1="12" x2="760" y2="12" className={styles.axis} />
        {lines.flatMap((line) => segments(points, line.key, max).map((segment, index) => <polyline key={`${line.key}-${index}`} points={segment} className={line.className} vectorEffect="non-scaling-stroke" />))}
      </svg>
      <div className={styles.chartTimes}><span>{formatMonitoringDate(points[0].at)}</span><span>{formatMonitoringDate(points[points.length - 1].at)}</span></div>
    </div>}
    {points.length > 0 ? <details className={styles.historyDetails}><summary>查看 {points.length} 次采样值</summary><DataTable minWidth={700} scrollLabel="ETL 历史采样值"><thead><tr><th scope="col">采样时间</th><th scope="col">输入消息/s</th><th scope="col">输出消息/s</th><th scope="col">错误/s</th><th scope="col">CPU</th><th scope="col">RSS 内存</th></tr></thead><tbody>{[...points].reverse().map((point) => <tr key={point.at}><td>{formatMonitoringDate(point.at)}</td><td>{formatETLValue(point.input_rate)}</td><td>{formatETLValue(point.output_rate)}</td><td>{formatETLValue(point.error_rate)}</td><td>{formatETLValue(point.cpu_percent, '%')}</td><td>{formatETLValue(point.rss_bytes, 'bytes')}</td></tr>)}</tbody></DataTable></details> : null}
  </Section>
}

function segments(points: ETLHistoryPoint[], key: 'input_rate' | 'output_rate', maximum: number) {
  const result: string[] = []
  let current: string[] = []
  const first = Date.parse(points[0].at)
  const duration = Math.max(1, Date.parse(points[points.length - 1].at) - first)
  for (const point of points) {
    const value = point[key]
    if (value === null) {
      if (current.length) result.push(current.join(' '))
      current = []
    } else current.push(`${(Date.parse(point.at) - first) / duration * 760},${168 - value / maximum * 156}`)
  }
  if (current.length) result.push(current.join(' '))
  return result
}
