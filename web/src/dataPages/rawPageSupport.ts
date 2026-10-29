import { formatShanghaiDateTime } from '../dateTime.js'
import { redactMonitoringJSON } from '../monitoring'

export type RawData = {
  id: number
  data_source_id: number
  external_id: string
  data_type: string
  raw_content: unknown
  rawContent?: unknown
  metadata: unknown
  status: string
  remark: string
  source: string
  created_at: number
  updated_at: number
}

export type WarehouseRawRecord = {
  id: number
  sourceID: number
  sourceCode: string
  status: 'received' | 'queued' | 'cleaning' | 'cleaned' | 'failed'
  traceID: string
  receivedAt: string
  createdAt: number
}

function publicText(value: unknown, maximumLength: number) {
  if (typeof value !== 'string') return ''
  const text = value.trim()
  return text.length <= maximumLength ? text : ''
}

export function parseWarehouseRawRecord(value: unknown): WarehouseRawRecord | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const record = value as Record<string, unknown>
  const id = Number(record.id)
  const sourceID = Number(record.source_id)
  const createdAt = Number(record.created_at)
  const status = publicText(record.status, 16)
  if (!Number.isInteger(id) || id <= 0 || !Number.isInteger(sourceID) || sourceID < 0
    || !Number.isInteger(createdAt) || createdAt < 0
    || !['received', 'queued', 'cleaning', 'cleaned', 'failed'].includes(status)) return null
  return { id, sourceID, sourceCode: publicText(record.source_code, 100), status: status as WarehouseRawRecord['status'], traceID: publicText(record.trace_id, 64), receivedAt: publicText(record.received_at, 32), createdAt }
}

export function parseRetransformResult(payload: unknown): { traceID: string; cleanRecordID: number } | null {
  if (!payload || typeof payload !== 'object') return null
  const data = (payload as { data?: unknown }).data
  if (!data || typeof data !== 'object' || Array.isArray(data)) return null
  const result = (data as { result?: unknown }).result
  if (!result || typeof result !== 'object' || Array.isArray(result)) return null
  const cleanRecordID = Number((result as Record<string, unknown>).clean_record_id)
  if (!Number.isInteger(cleanRecordID) || cleanRecordID <= 0) return null
  return { traceID: publicText((result as Record<string, unknown>).trace_id, 64), cleanRecordID }
}

export function backendDateTime(value: string) {
  const normalized = value.trim().replace('T', ' ')
  return /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(normalized) ? `${normalized}:00` : normalized
}

export function formatUnixTime(value: number) {
  return value ? formatShanghaiDateTime(value * 1000, '-') : '-'
}

function parseMaybeJSON(value: unknown) {
  if (!value) return null
  if (typeof value === 'object') return value
  if (typeof value !== 'string') return null
  try { return JSON.parse(value) as unknown } catch { return null }
}

export function rawDataOrigin(record: RawData) {
  const metadata = parseMaybeJSON(record.metadata)
  const values = metadata && typeof metadata === 'object' && !Array.isArray(metadata) ? metadata as Record<string, unknown> : null
  if (values?.format === 'fetch') return 'fetch'
  if (typeof values?.format === 'string') return values.format
  if (record.source) return record.source
  if (record.remark) return record.remark
  if (typeof values?.source === 'string') return values.source
  if (typeof values?.remark === 'string') return values.remark
  return 'ingest'
}

export function redactedRawData(record: RawData) {
  return redactMonitoringJSON({ raw_content: record.raw_content ?? record.rawContent ?? null, metadata: record.metadata ?? null })
}

export function warehouseStatusLabel(status: WarehouseRawRecord['status']) {
  return ({ received: '已接收', queued: '排队中', cleaning: '处理中', cleaned: '已清洗', failed: '失败' } as const)[status]
}

export function rawStatusTone(status: string) {
  if (/error|failed|异常|失败/i.test(status)) return 'danger' as const
  if (/processed|cleaned|received|完成|已清洗|已接收/i.test(status)) return 'success' as const
  if (/pending|processing|queued|cleaning|待处理|处理中|排队/i.test(status)) return 'warning' as const
  return 'neutral' as const
}
