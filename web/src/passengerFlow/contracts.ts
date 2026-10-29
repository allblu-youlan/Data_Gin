export type PassengerFilter = { startDate: string; endDate: string; mallId: string }
export type PassengerMall = { mallId: number; mallName: string; shopId: string }
export type PassengerMetrics = {
  passby: number
  traffic: number
  trafficCount: number
  trafficBatch: number
  captureRate: number
  recordCount: number
}
export type PassengerTrend = PassengerMetrics & { hourStart: string }
export type PassengerMallSummary = PassengerMetrics & PassengerMall & { shopName: string }
export type PassengerOverview = {
  startDate: string
  endDate: string
  timeZone: string
  summary: PassengerMetrics & { mallCount: number; latestHour: string | null }
  trend: PassengerTrend[]
  malls: PassengerMallSummary[]
}

const hour = 3_600_000
const day = 24 * hour
export const xlsxContentType = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'

export function shanghaiPassengerDate(now = new Date()) {
  return new Date(now.getTime() + 8 * hour).toISOString().slice(0, 10)
}

export function passengerPreset(days: number, now = new Date()): PassengerFilter {
  const endDate = shanghaiPassengerDate(now)
  return { startDate: new Date(Date.parse(endDate) - (days - 1) * day).toISOString().slice(0, 10), endDate, mallId: '' }
}

function validDate(value: string) {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value
}

export function passengerFilterError(filter: PassengerFilter) {
  if (!validDate(filter.startDate) || !validDate(filter.endDate)) return '请选择有效的开始和结束日期。'
  const duration = Date.parse(filter.endDate) - Date.parse(filter.startDate)
  if (duration < 0) return '结束日期不能早于开始日期。'
  if (duration >= 31 * day) return '每次最多查询或下载 31 天，请缩短日期范围。'
  if (filter.mallId && (!/^[1-9]\d*$/.test(filter.mallId) || !Number.isSafeInteger(Number(filter.mallId)))) return '请选择有效的门店。'
  return ''
}

export function passengerFilterBody(filter: PassengerFilter) {
  const error = passengerFilterError(filter)
  if (error) throw new Error(error)
  return { startDate: filter.startDate, endDate: filter.endDate, ...(filter.mallId ? { mallId: Number(filter.mallId) } : {}) }
}

export function passengerQueryPath(filter: PassengerFilter) {
  const body = passengerFilterBody(filter)
  const query = new URLSearchParams({ startDate: body.startDate, endDate: body.endDate })
  if (body.mallId) query.set('mallId', String(body.mallId))
  return `/v1/kaiyu-passenger?${query}`
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function dataOf(value: unknown) {
  return record(value) && record(value.data) ? value.data : null
}

function count(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function metrics(value: unknown): value is PassengerMetrics & Record<string, unknown> {
  return record(value) && ['passby', 'traffic', 'trafficCount', 'trafficBatch', 'recordCount'].every(key => count(value[key]))
    && typeof value.captureRate === 'number' && Number.isFinite(value.captureRate) && value.captureRate >= 0
}

function mall(value: unknown): value is PassengerMall & Record<string, unknown> {
  return record(value) && count(value.mallId) && value.mallId > 0 && typeof value.mallName === 'string' && typeof value.shopId === 'string'
}

function hourStart(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:00:00\+08:00$/.test(value) && Number.isFinite(Date.parse(value))
}

export function parsePassengerMalls(payload: unknown): PassengerMall[] | null {
  const data = dataOf(payload)
  return data && Array.isArray(data.items) && data.items.every(mall) ? data.items : null
}

export function parsePassengerOverview(payload: unknown): PassengerOverview | null {
  const data = dataOf(payload)
  if (!data || typeof data.startDate !== 'string' || typeof data.endDate !== 'string'
    || passengerFilterError({ startDate: data.startDate, endDate: data.endDate, mallId: '' })
    || data.timeZone !== 'Asia/Shanghai' || !record(data.summary) || !metrics(data.summary)
    || !count(data.summary.mallCount) || (data.summary.latestHour !== null && !hourStart(data.summary.latestHour))
    || !Array.isArray(data.trend) || !data.trend.every(row => metrics(row) && record(row) && hourStart(row.hourStart))
    || !Array.isArray(data.malls) || !data.malls.every(row => metrics(row) && mall(row) && record(row) && typeof row.shopName === 'string')) return null
  return data as PassengerOverview
}

// Insert missing hours as gaps, never as zero passenger counts.
export function passengerChartData(trend: PassengerTrend[], metric: 'traffic' | 'passby' | 'captureRate') {
  if (!trend.length) return []
  const byTime = new Map(trend.map(point => [Date.parse(point.hourStart), point]))
  const times = [...byTime.keys()].sort((a, b) => a - b)
  const result: Array<{ time: string; value?: number }> = []
  for (let time = times[0]; time <= times[times.length - 1]; time += hour) {
    const point = byTime.get(time)
    const value = point && (metric !== 'captureRate' || point.passby > 0) ? point[metric] : undefined
    result.push({ time: new Date(time + 8 * hour).toISOString().slice(0, 19) + '+08:00', value: metric === 'captureRate' && value !== undefined ? value * 100 : value })
  }
  return result
}

export function passengerWorkbookName(filter: PassengerFilter) {
  passengerFilterBody(filter)
  return `监控客流_${filter.startDate}_${filter.endDate}_${filter.mallId || '全部门店'}.xlsx`
}

export function savePassengerWorkbook(blob: Blob, filter: PassengerFilter) {
  if (blob.type.split(';')[0] !== xlsxContentType || blob.size === 0) throw new Error('下载内容不是有效的 XLSX 文件，请重试。')
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = passengerWorkbookName(filter)
  anchor.hidden = true
  document.body.append(anchor)
  try { anchor.click() } finally {
    anchor.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 1_000)
  }
}
