import { DataTable, MetricStrip, Section, StatusTag, type StatusTagTone } from '../../ui'
import { formatMonitoringDate } from '../contracts'
import { etlProbeLabel, formatETLValue, type ETLIndicator, type ETLMonitorSnapshot, type ETLProbe } from './contracts'
import styles from './ETLMonitorPage.module.css'

function probeTone(probe: ETLProbe): StatusTagTone {
  if (probe.state === 'healthy') return 'success'
  if (probe.state === 'unhealthy') return 'danger'
  if (probe.state === 'failing' || probe.state === 'recovering') return 'warning'
  return 'neutral'
}

export function ETLProbes({ snapshot }: { snapshot: ETLMonitorSnapshot }) {
  const probes = [
    { key: 'ping' as const, label: '服务存活', description: 'HTTP 服务响应', path: '/ping' },
    { key: 'ready' as const, label: '组件就绪', description: '输入、输出连接状态', path: '/ready' },
    { key: 'metrics' as const, label: '指标采集', description: 'Prometheus 指标读取', path: '/metrics' },
  ]
  return <Section title="服务探测" description={`健康检查 ${snapshot.health_interval_seconds} 秒 / 指标采集 ${snapshot.metrics_interval_seconds} 秒。连续失败 3 次确认异常，连续成功 2 次确认恢复。`} flush>
    <div className={styles.probes}>{probes.map(({ key, label, description, path }) => {
      const probe = snapshot.probes[key]
      return <article key={key} className={styles.probe}>
        <div className={styles.probeHeading}><h3>{label}</h3><StatusTag tone={probeTone(probe)}>{etlProbeLabel(probe)}</StatusTag></div>
        <p>{description} <code>{path}</code></p>
        <dl>
          <div><dt>本次响应</dt><dd>{probe.checked_at ? `HTTP ${probe.http_status || '—'} · ${formatETLValue(probe.duration_ms, 'ms')}` : '—'}</dd></div>
          <div><dt>检查时间</dt><dd>{formatMonitoringDate(probe.checked_at)}</dd></div>
          <div><dt>最近探测成功</dt><dd>{formatMonitoringDate(probe.last_success_at)}</dd></div>
          <div><dt>连续失败 / 成功</dt><dd>{probe.consecutive_failures} / {probe.consecutive_successes}</dd></div>
        </dl>
        {probe.error ? <p className={styles.probeError}>{probe.error}</p> : null}
      </article>
    })}</div>
  </Section>
}

const headlineKeys = ['input_rate', 'output_rate', 'error_rate', 'cpu_percent', 'rss_bytes', 'uptime_seconds']
const runtimeKeys = new Set(['rss_bytes', 'heap_bytes', 'goroutines', 'gc_pause_ms', 'cpu_percent', 'uptime_seconds', 'open_fds'])
export function ETLMeasurements({ summary, stale }: { summary: ETLIndicator[]; stale: boolean }) {
  const headline = headlineKeys.map((key) => summary.find((item) => item.key === key)).filter((item): item is ETLIndicator => !!item)
  const processing = summary.filter((item) => !runtimeKeys.has(item.key))
  const runtime = summary.filter((item) => runtimeKeys.has(item.key))
  return <>
    {headline.length > 0 ? <MetricStrip label={stale ? '最近采集关键指标（已陈旧）' : 'ETL 关键指标'} items={headline.map((item) => ({ key: item.key, label: item.label, value: formatETLValue(item.value, item.unit), detail: item.value === null ? '等待可用采样' : undefined }))} /> : null}
    <div className={styles.measurements}>
      <Section title="消息与组件" description="累计计数随 ETL 进程重启重置；速率需要至少两次有效采样。缺失指标显示 —。" flush><IndicatorTable indicators={processing} label="消息与组件指标" /></Section>
      <Section title="进程与 Go 运行时" description="CPU 为单核使用百分比，可超过 100%；运行时指标需在 ETL 端开启。" flush><IndicatorTable indicators={runtime} label="进程与 Go 运行时指标" /></Section>
    </div>
  </>
}
function IndicatorTable({ indicators, label }: { indicators: ETLIndicator[]; label: string }) {
  return <DataTable minWidth={400} scrollLabel={label}><thead><tr><th scope="col">指标</th><th scope="col">数值</th></tr></thead><tbody>{indicators.map((item) => <tr key={item.key}><td><strong>{item.label}</strong><small className={styles.metricDescription}>{item.description}</small></td><td className={styles.numeric}>{formatETLValue(item.value, item.unit)}</td></tr>)}</tbody></DataTable>
}
