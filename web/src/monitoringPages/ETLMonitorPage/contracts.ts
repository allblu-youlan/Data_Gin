export type ETLProbeState = 'unknown' | 'healthy' | 'failing' | 'unhealthy' | 'recovering'
export interface ETLProbe {
  state: ETLProbeState
  success: boolean | null
  http_status: number
  error: string
  checked_at: string | null
  last_success_at: string | null
  duration_ms: number
  consecutive_failures: number
  consecutive_successes: number
  alerting: boolean
}
export interface ETLIndicator { key: string; label: string; value: number | null; unit: string; description: string }
export interface ETLSeries { name: string; type: string; labels: Record<string, string>; value: number | null; rate_per_second: number | null }
export interface ETLHistoryPoint { at: string; input_rate: number | null; output_rate: number | null; error_rate: number | null; cpu_percent: number | null; rss_bytes: number | null }
export interface ETLMonitorSnapshot {
  configured: boolean
  config_error: string
  target: string
  checked_at: string | null
  last_metrics_at: string | null
  stale: boolean
  health_interval_seconds: number
  metrics_interval_seconds: number
  probes: { ping: ETLProbe; ready: ETLProbe; metrics: ETLProbe }
  summary: ETLIndicator[]
  series: ETLSeries[]
  history: ETLHistoryPoint[]
}

export function parseETLMonitor(value: unknown): ETLMonitorSnapshot | null {
  const snapshot = record(record(value)?.data)
  if (!snapshot || typeof snapshot.configured !== 'boolean' || typeof snapshot.stale !== 'boolean'
    || typeof snapshot.target !== 'string' || typeof snapshot.config_error !== 'string'
    || !dateOrNull(snapshot.checked_at) || !dateOrNull(snapshot.last_metrics_at)
    || !finite(snapshot.health_interval_seconds) || !finite(snapshot.metrics_interval_seconds)) return null
  const probes = record(snapshot.probes)
  if (!probes || !['ping', 'ready', 'metrics'].every((key) => isProbe(probes[key]))) return null
  if (!Array.isArray(snapshot.summary) || !snapshot.summary.every(isIndicator)
    || !Array.isArray(snapshot.series) || !snapshot.series.every(isSeries)
    || !Array.isArray(snapshot.history) || !snapshot.history.every(isHistoryPoint)) return null
  return snapshot as unknown as ETLMonitorSnapshot
}

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
}
function finite(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) }
function numberOrNull(value: unknown) { return value === null || finite(value) }
function dateOrNull(value: unknown) { return value === null || typeof value === 'string' && Number.isFinite(Date.parse(value)) }
function isProbe(value: unknown) {
  const probe = record(value)
  return !!probe && ['unknown', 'healthy', 'failing', 'unhealthy', 'recovering'].includes(String(probe.state))
    && (probe.success === null || typeof probe.success === 'boolean') && typeof probe.alerting === 'boolean'
    && typeof probe.error === 'string' && dateOrNull(probe.checked_at) && dateOrNull(probe.last_success_at)
    && ['http_status', 'duration_ms', 'consecutive_failures', 'consecutive_successes'].every((key) => finite(probe[key]) && Number(probe[key]) >= 0)
}
function isIndicator(value: unknown) {
  const indicator = record(value)
  return !!indicator && ['key', 'label', 'unit', 'description'].every((key) => typeof indicator[key] === 'string') && numberOrNull(indicator.value)
}
function isSeries(value: unknown) {
  const series = record(value)
  const labels = record(series?.labels)
  return !!series && typeof series.name === 'string' && typeof series.type === 'string'
    && labels !== null && Object.values(labels).every((label) => typeof label === 'string')
    && numberOrNull(series.value) && numberOrNull(series.rate_per_second)
}
function isHistoryPoint(value: unknown) {
  const point = record(value)
  return !!point && typeof point.at === 'string' && Number.isFinite(Date.parse(point.at))
    && ['input_rate', 'output_rate', 'error_rate', 'cpu_percent', 'rss_bytes'].every((key) => numberOrNull(point[key]))
}

export function formatETLValue(value: number | null, unit = ''): string {
  if (value === null) return '—'
  if (unit === 'bytes' || unit === 'B') {
    const power = Math.min(3, Math.max(0, Math.floor(Math.log2(Math.abs(value) || 1) / 10)))
    return `${formatNumber(value / 1024 ** power)} ${['B', 'KiB', 'MiB', 'GiB'][power]}`
  }
  return `${formatNumber(value)}${unit ? ` ${unit}` : ''}`
}
function formatNumber(value: number) { return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(value) }

export function filterETLSeries(series: ETLSeries[], query: string): ETLSeries[] {
  const keyword = query.trim().toLowerCase()
  return series.filter((item) => !keyword || `${item.name} ${Object.entries(item.labels).map(([key, value]) => `${key}=${value}`).join(' ')}`.toLowerCase().includes(keyword))
}
export function etlProbeLabel(probe: ETLProbe): string {
  return { unknown: '尚未探测', healthy: '正常', failing: '探测失败 · 待确认', unhealthy: '持续异常', recovering: '恢复确认中' }[probe.state]
}
