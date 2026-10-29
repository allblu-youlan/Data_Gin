import { formatShanghaiDateTime, parseShanghaiDateTime } from '../dateTime.js'
import { redactMonitoringJSON } from '../monitoring'

export type ProcessedData = {
  id: number
  raw_data_id: number
  data_type: string
  data_fields: string
  quality_score: number
  created_at: number
}

export type CleanRecord = {
  id: number
  raw_record_id: number
  source_id: number
  table_name: string
  business_key: string
  quality_score: number
  status: string
  created_at: number
}

export function unixTimestamp(value: string) {
  if (!value) return ''
  const date = parseShanghaiDateTime(value)
  return date ? String(Math.floor(date.getTime() / 1000)) : ''
}

export function formatUnixTime(value: number) {
  return value ? formatShanghaiDateTime(value * 1000, '-') : '-'
}

export function formatQualityScore(value: number) {
  return Number.isFinite(value) ? `${value.toFixed(1)} / 100` : '-'
}

export function cleanRecordStatusLabel(status: string) {
  if (status === 'ready') return '待推送'
  if (status === 'invalid') return '无效'
  if (status === 'delivered') return '已交付'
  return status || '-'
}

export function cleanRecordStatusTone(status: string) {
  if (status === 'invalid') return 'danger' as const
  if (status === 'delivered') return 'success' as const
  if (status === 'ready') return 'warning' as const
  return 'neutral' as const
}

export function qualityTone(value: number) {
  return value >= 80 ? 'success' as const : 'warning' as const
}

export function processedFields(value: string) {
  if (!value) return {}
  try {
    return redactMonitoringJSON(JSON.parse(value) as unknown)
  } catch {
    return redactMonitoringJSON(value)
  }
}
