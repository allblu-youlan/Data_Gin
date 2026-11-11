import { passengerFilterBody, passengerFilterError, xlsxContentType, type PassengerFilter } from '../passengerFlow/contracts.js'

export type RegionFilter = PassengerFilter & { regionName: string }
export type RegionItem = {
  mallId: number; mallName: string; shopId: string; statDate: string; regionName: string
  exposureCount: number; stayCount: number; stayRate: number; avgStayTime: number; fetchedAt: string
}
export type RegionOverview = {
  startDate: string; endDate: string; timezone: string; items: RegionItem[]
  summary: { exposureCount: number; stayCount: number; recordCount: number; mallCount: number; regionCount: number; latestFetchedAt: string | null }
}
export type RegionState = 'success' | 'empty' | 'failed' | 'unqueried'
export type RegionStatus = {
  mallId: number; mallName: string; shopId: string; statDate: string; state: RegionState
  recordCount: number; fetchedAt?: string; lastSuccessAt?: string; errorMessage?: string
}
export type RegionMissing = {
  startDate: string; endDate: string; timezone: string
  expectedDays: number; successDays: number; emptyDays: number; failedDays: number; unqueriedDays: number; unboundMalls: number
  items: RegionStatus[]
}
export type RegionBackfillBatch = {
  processedDays: number; savedDays: number; emptyDays: number; failedDays: number; sourceMissingDays: number
  done: boolean; nextCursor: string
}
export type RegionGroup = Pick<RegionItem, 'mallId' | 'mallName' | 'shopId' | 'regionName' | 'exposureCount' | 'stayCount'> & { key: string; stayRate?: number; recordCount: number }
export const regionStateLabels: Record<RegionState, string> = { success: '已取到数据', empty: '源端未返回', failed: '取数失败', unqueried: '未查询' }
export function regionFilterBody(filter: RegionFilter) {
  return { ...passengerFilterBody(filter), ...(filter.regionName ? { regionName: filter.regionName } : {}) }
}
export function regionQueryPath(filter: RegionFilter, suffix = '') {
  const body = regionFilterBody(filter)
  const query = new URLSearchParams({ startDate: body.startDate, endDate: body.endDate })
  if (body.mallId) query.set('mallId', String(body.mallId))
  if (body.regionName) query.set('regionName', body.regionName)
  return `/v1/kaiyu-region-stay${suffix}?${query}`
}
function record(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value) }
function dataOf(value: unknown) { return record(value) && record(value.data) ? value.data : null }
function count(value: unknown) { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 }
function decimal(value: unknown) { return typeof value === 'number' && Number.isFinite(value) && value >= 0 }
function timestamp(value: unknown) { return typeof value === 'string' && Number.isFinite(Date.parse(value)) }
function date(value: unknown) { return typeof value === 'string' && !passengerFilterError({ startDate: value, endDate: value, mallId: '' }) }
function range(data: Record<string, unknown>) {
  return typeof data.startDate === 'string' && typeof data.endDate === 'string' && data.timezone === 'Asia/Shanghai'
    && !passengerFilterError({ startDate: data.startDate, endDate: data.endDate, mallId: '' })
}
function identity(value: unknown): value is Record<string, unknown> {
  return record(value) && count(value.mallId) && Number(value.mallId) > 0 && typeof value.mallName === 'string'
    && typeof value.shopId === 'string' && date(value.statDate)
}
export function parseRegionOverview(payload: unknown): RegionOverview | null {
  const data = dataOf(payload)
  if (!data || !range(data) || !record(data.summary)
    || !['exposureCount', 'stayCount', 'recordCount', 'mallCount', 'regionCount'].every(key => count((data.summary as Record<string, unknown>)[key]))
    || (data.summary.latestFetchedAt !== null && !timestamp(data.summary.latestFetchedAt))
    || !Array.isArray(data.items) || !data.items.every(row => identity(row) && typeof row.regionName === 'string'
      && count(row.exposureCount) && count(row.stayCount) && decimal(row.stayRate) && decimal(row.avgStayTime) && timestamp(row.fetchedAt))) return null
  return data as RegionOverview
}
export function parseRegionMissing(payload: unknown): RegionMissing | null {
  const data = dataOf(payload)
  if (!data || !range(data) || !['expectedDays', 'successDays', 'emptyDays', 'failedDays', 'unqueriedDays', 'unboundMalls'].every(key => count(data[key]))
    || !Array.isArray(data.items) || !data.items.every(row => identity(row) && typeof row.state === 'string'
      && Object.prototype.hasOwnProperty.call(regionStateLabels, row.state) && count(row.recordCount)
      && (row.fetchedAt === undefined || timestamp(row.fetchedAt)) && (row.lastSuccessAt === undefined || timestamp(row.lastSuccessAt))
      && (row.errorMessage === undefined || typeof row.errorMessage === 'string'))) return null
  return data as RegionMissing
}
export function parseRegionBackfill(payload: unknown): RegionBackfillBatch | null {
  const data = dataOf(payload)
  if (!data || !['processedDays', 'savedDays', 'emptyDays', 'failedDays', 'sourceMissingDays'].every(key => count(data[key]))
    || typeof data.done !== 'boolean' || typeof data.nextCursor !== 'string' || data.done !== (data.nextCursor === '')) return null
  return data as RegionBackfillBatch
}
export function regionKey(row: Pick<RegionItem, 'shopId' | 'regionName'>) { return JSON.stringify([row.shopId, row.regionName]) }
export function regionGroups(items: RegionItem[]): RegionGroup[] {
  const groups = new Map<string, RegionGroup>()
  for (const item of items) {
    const key = regionKey(item)
    const group = groups.get(key) || { key, mallId: item.mallId, mallName: item.mallName, shopId: item.shopId, regionName: item.regionName, exposureCount: 0, stayCount: 0, recordCount: 0 }
    group.exposureCount += item.exposureCount
    group.stayCount += item.stayCount
    group.recordCount++
    groups.set(key, group)
  }
  return [...groups.values()].map(group => ({ ...group, stayRate: group.exposureCount > 0 ? group.stayCount / group.exposureCount : undefined }))
    .sort((a, b) => b.stayCount - a.stayCount || a.mallId - b.mallId || a.regionName.localeCompare(b.regionName))
}
export function regionDailyChart(items: RegionItem[], filter: PassengerFilter, metric: 'exposureCount' | 'stayCount' | 'stayRate' | 'avgStayTime') {
  passengerFilterBody(filter)
  const days = new Map<string, RegionItem[]>()
  for (const row of items) { const values = days.get(row.statDate) || []; values.push(row); days.set(row.statDate, values) }
  const points: Array<{ time: string; value?: number }> = []
  for (let time = Date.parse(filter.startDate); time <= Date.parse(filter.endDate); time += 86_400_000) {
    const day = new Date(time).toISOString().slice(0, 10)
    const rows = days.get(day)
    let value: number | undefined
    if (rows?.length) {
      if (metric === 'avgStayTime') value = rows.length === 1 ? rows[0].avgStayTime : undefined
      else if (metric === 'stayRate') {
        const exposure = rows.reduce((sum, row) => sum + row.exposureCount, 0)
        if (exposure > 0) value = rows.reduce((sum, row) => sum + row.stayCount, 0) / exposure * 100
      } else value = rows.reduce((sum, row) => sum + row[metric], 0)
    }
    points.push({ time: `${day}T00:00:00+08:00`, value })
  }
  return points
}
export async function runRegionBackfill(send: (cursor: string) => Promise<RegionBackfillBatch>, onProgress: (total: RegionBackfillBatch) => void, shouldStop: () => boolean) {
  let total: RegionBackfillBatch = { processedDays: 0, savedDays: 0, emptyDays: 0, failedDays: 0, sourceMissingDays: 0, done: false, nextCursor: '' }
  const seen = new Set<string>()
  do {
    const batch = await send(total.nextCursor)
    if (!batch.done && (!batch.nextCursor || seen.has(batch.nextCursor))) throw new Error('补拉进度未前进，请重新检查同步状态。')
    seen.add(batch.nextCursor)
    total = { ...batch, processedDays: total.processedDays + batch.processedDays, savedDays: total.savedDays + batch.savedDays,
      emptyDays: total.emptyDays + batch.emptyDays, failedDays: total.failedDays + batch.failedDays, sourceMissingDays: total.sourceMissingDays + batch.sourceMissingDays }
    onProgress(total)
    if (!total.done && !shouldStop()) await new Promise(resolve => setTimeout(resolve, 600))
  } while (!total.done && !shouldStop())
  return total
}
export function saveRegionWorkbook(blob: Blob, filter: RegionFilter) {
  if (blob.type.split(';')[0] !== xlsxContentType || !blob.size) throw new Error('下载内容不是有效的 XLSX 文件，请重试。')
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url; link.download = `店内区域_${filter.startDate}_${filter.endDate}_${filter.mallId || '全部门店'}.xlsx`; link.hidden = true
  document.body.append(link)
  try { link.click() } finally { link.remove(); window.setTimeout(() => URL.revokeObjectURL(url), 1_000) }
}
