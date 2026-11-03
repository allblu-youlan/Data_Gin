export type SalesSummaryMetrics = {
  salesAmount: number
  salesTarget: number
  salesAchievementRate: number
  averageOrderValue: number
  itemsPerOrder: number
  quantityTarget: number
  conversionRate: number
  memberSalesRatio: number
  totalQuantity: number
  salesQuantity: number
  returnQuantity: number
  orderCount: number
  averageItemPrice: number
  discountRate: number
}

export type SalesSummary = {
  startDate: string
  endDate: string
  mall: { id: number; mallCode: string; nameCn: string }
  metrics: SalesSummaryMetrics
  weather: null | { date: string; temperatureMinC: number | null; temperatureMaxC: number | null; skycon: string }
}

export type SalesPeriod = 'today' | 'week' | 'month' | 'custom'
export type SalesDateRange = { startDate: string; endDate: string }

const metricNames: Array<keyof SalesSummaryMetrics> = [
  'salesAmount', 'salesTarget', 'salesAchievementRate', 'averageOrderValue',
  'itemsPerOrder', 'quantityTarget', 'conversionRate', 'memberSalesRatio',
  'totalQuantity', 'salesQuantity', 'returnQuantity', 'orderCount', 'averageItemPrice', 'discountRate',
]

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

export function isSalesDate(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const date = new Date(`${value}T00:00:00Z`)
  return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value
}

export function salesSummaryPath(mallCode: string, range: SalesDateRange) {
  const query = new URLSearchParams({ mallCode, ...range })
  return `/v1/sales-summary?${query.toString()}`
}

export function parseSalesSummary(payload: unknown, mallCode: string, range: SalesDateRange): SalesSummary | null {
  if (!isRecord(payload) || (payload.code !== 0 && payload.code !== 200) || !isRecord(payload.data)) return null
  const data = payload.data
  if (data.startDate !== range.startDate || data.endDate !== range.endDate || !isRecord(data.mall) ||
    data.mall.mallCode !== mallCode || typeof data.mall.nameCn !== 'string' || !Number.isSafeInteger(data.mall.id) ||
    !isRecord(data.metrics)) return null
  const values = data.metrics
  if (metricNames.some((name) => typeof values[name] !== 'number' || !Number.isFinite(values[name]))) return null
  const weather = data.weather
  if (weather !== null && (!isRecord(weather) || typeof weather.date !== 'string' || typeof weather.skycon !== 'string' ||
    ![weather.temperatureMinC, weather.temperatureMaxC].every((value) => value === null || (typeof value === 'number' && Number.isFinite(value))))) return null
  return data as SalesSummary
}

export function shanghaiSalesDate(now = new Date()) {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now)
  return ['year', 'month', 'day'].map((type) => parts.find((part) => part.type === type)?.value).join('-')
}

export function salesPeriodRange(period: Exclude<SalesPeriod, 'custom'>, today = shanghaiSalesDate()): SalesDateRange {
  const date = new Date(`${today}T00:00:00Z`)
  if (period === 'week') date.setUTCDate(date.getUTCDate() - ((date.getUTCDay() + 6) % 7))
  if (period === 'month') date.setUTCDate(1)
  return { startDate: date.toISOString().slice(0, 10), endDate: today }
}
