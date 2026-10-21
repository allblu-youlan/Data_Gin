export type MonitoringPagination = {
  page: number
  pageSize: number
  total: number
  totalPages: number
}

export type MonitoringPage<T> = {
  list: T[]
  pagination: MonitoringPagination
}

export type RunListQuery = {
  page: number
  pageSize: number
  status?: string
  runType?: string
  traceID?: string
  startTime?: string
  endTime?: string
}

export type DeliveryLogListQuery = {
  page: number
  pageSize: number
  destination?: string
  source?: string
  success?: '' | 'true' | 'false'
  businessKey?: string
  startTime?: string
  endTime?: string
}

export type ConfigurationListQuery = {
  page: number
  pageSize: number
  keyword?: string
  enabled?: '' | 'true' | 'false'
}

export type SourceListQuery = ConfigurationListQuery & { sourceType?: string }
export type TransformRuleListQuery = ConfigurationListQuery & { ruleType?: string; sourceID?: string }
export type DestinationListQuery = ConfigurationListQuery & { destinationType?: string }
export type DeliveryTaskListQuery = ConfigurationListQuery & { destinationID?: string }

export type ExcelMatchJobListQuery = {
  page: number
  pageSize: number
  keyword?: string
  status?: string
  operation?: string
}

const runStatuses = ['running', 'success', 'failed', 'partial_success']
const runTypes = ['fetch', 'ingest', 'transform', 'delivery']

function boundedPositive(value: number, fallback: number, maximum: number) {
  return Number.isInteger(value) && value > 0 && value <= maximum ? value : fallback
}

function appendText(params: URLSearchParams, name: string, value: string | undefined, maximum: number) {
  const normalized = value?.trim() ?? ''
  if (normalized) params.set(name, normalized.slice(0, maximum))
}

function baseQuery(page: number, pageSize: number) {
  const params = new URLSearchParams()
  params.set('page', String(boundedPositive(page, 1, 1_000_000)))
  params.set('page_size', String(boundedPositive(pageSize, 20, 100)))
  return params
}

export function normalizeMonitoringPageNumber(requestedPage: number, totalPages: number) {
  const page = boundedPositive(requestedPage, 1, 1_000_000)
  return Number.isInteger(totalPages) && totalPages > 0 ? Math.min(page, totalPages) : 1
}

export function buildRunListQuery(query: RunListQuery) {
  const params = baseQuery(query.page, query.pageSize)
  if (runStatuses.includes(query.status ?? '')) params.set('status', query.status!)
  if (runTypes.includes(query.runType ?? '')) params.set('run_type', query.runType!)
  appendText(params, 'trace_id', query.traceID, 64)
  appendText(params, 'start_time', query.startTime, 32)
  appendText(params, 'end_time', query.endTime, 32)
  return params.toString()
}

export function buildDeliveryLogListQuery(query: DeliveryLogListQuery) {
  const params = baseQuery(query.page, query.pageSize)
  appendText(params, 'destination', query.destination, 100)
  appendText(params, 'source', query.source, 100)
  if (query.success === 'true' || query.success === 'false') params.set('success', query.success)
  appendText(params, 'business_key', query.businessKey, 255)
  appendText(params, 'start_time', query.startTime, 32)
  appendText(params, 'end_time', query.endTime, 32)
  return params.toString()
}

function configurationQuery(query: ConfigurationListQuery) {
  const params = baseQuery(query.page, query.pageSize)
  appendText(params, 'keyword', query.keyword, 100)
  if (query.enabled === 'true' || query.enabled === 'false') params.set('enabled', query.enabled)
  return params
}

export function buildSourceListQuery(query: SourceListQuery) {
  const params = configurationQuery(query)
  appendText(params, 'source_type', query.sourceType, 50)
  return params.toString()
}

export function buildTransformRuleListQuery(query: TransformRuleListQuery) {
  const params = configurationQuery(query)
  appendText(params, 'rule_type', query.ruleType, 50)
  if (/^[1-9]\d*$/.test(query.sourceID?.trim() ?? '')) params.set('source_id', query.sourceID!.trim())
  return params.toString()
}

export function buildDestinationListQuery(query: DestinationListQuery) {
  const params = configurationQuery(query)
  appendText(params, 'destination_type', query.destinationType, 50)
  return params.toString()
}

export function buildDeliveryTaskListQuery(query: DeliveryTaskListQuery) {
  const params = configurationQuery(query)
  if (/^[1-9]\d*$/.test(query.destinationID?.trim() ?? '')) params.set('destination_id', query.destinationID!.trim())
  return params.toString()
}

export function buildExcelMatchJobListQuery(query: ExcelMatchJobListQuery) {
  const params = baseQuery(query.page, query.pageSize)
  appendText(params, 'keyword', query.keyword, 255)
  if (['pending', 'running', 'success', 'failed', 'expired'].includes(query.status ?? '')) params.set('status', query.status!)
  const operation = query.operation?.trim() ?? ''
  if (operation === 'match' || operation === 'write' || operation === 'export') params.set('operation', operation)
  return params.toString()
}

export function parseMonitoringPage<T>(payload: unknown, key: string): MonitoringPage<T> | null {
  if (!payload || typeof payload !== 'object' || !key) return null
  const data = (payload as { data?: unknown }).data
  if (!data || typeof data !== 'object' || Array.isArray(data)) return null
  const record = data as Record<string, unknown>
  if (!Array.isArray(record[key]) || !record.pagination || typeof record.pagination !== 'object' || Array.isArray(record.pagination)) return null
  const pagination = record.pagination as Record<string, unknown>
  const page = Number(pagination.page)
  const pageSize = Number(pagination.page_size)
  const total = Number(pagination.total)
  const totalPages = Number(pagination.total_pages)
  if (!Number.isInteger(page) || page < 1 || !Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100
    || !Number.isInteger(total) || total < 0 || !Number.isInteger(totalPages) || totalPages < 0) return null
  return { list: record[key] as T[], pagination: { page, pageSize, total, totalPages } }
}
