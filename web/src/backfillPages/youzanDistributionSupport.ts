import { formatShanghaiDate } from '../dateTime.js'
import type { ClientResponse } from '../api/client'

export type LegacyTask = {
  code: string
  name: string
  category: string
  source_code: string
  source_name: string
  cron_expr: string
  input_table: string
  output_table: string
  target_system: string
  description: string
}

export type LegacyTaskRunResult = {
  id: string
  queue: string
  type: string
}

export type YouzanDistributionBackfillSample = {
  tid: string
  status: string
  reason: string
  success_time: string
  payment: string
  fans_nickname: string
}

export type YouzanDistributionTimeFilter = 'created' | 'success'

export type YouzanDistributionBackfillPayload = {
  time_filter: YouzanDistributionTimeFilter
  start_time: string
  end_time: string
}

export type YouzanDistributionBackfillResult = {
  time_filter: YouzanDistributionTimeFilter
  start_time: string
  end_time: string
  page_size: number
  fetch_pages: number
  total_count: number
  preview_count: number
  writable_count: number
  saved_count: number
  existing_count: number
  failed_count: number
  samples: YouzanDistributionBackfillSample[]
}

export function readResponseObject<T>(response: ClientResponse, key: string): T | null {
  if (!response.data || typeof response.data !== 'object') return null
  const data = (response.data as { data?: Record<string, unknown> }).data
  const value = data?.[key]
  return value && typeof value === 'object' ? value as T : null
}

export function formValue(form: FormData, key: string) {
  const value = form.get(key)
  return typeof value === 'string' ? value : ''
}

export function backendDateTime(value: string) {
  const normalized = value.trim().replace('T', ' ')
  return /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(normalized) ? `${normalized}:00` : normalized
}

export function previousDayDateTimeLocal(endOfDay: boolean) {
  const date = formatShanghaiDate(new Date(Date.now() - 24 * 60 * 60 * 1000))
  return `${date}T${endOfDay ? '23:59:59' : '00:00:00'}`
}

export function timeFilterLabel(value: YouzanDistributionTimeFilter) {
  return value === 'success' ? '订单完成时间' : '下单时间'
}

export function backfillStatusLabel(value: string) {
  const labels: Record<string, string> = {
    pending: '待写入',
    created: '已写入',
    exists: '已存在',
    invalid: '无效',
    failed: '失败',
  }
  return labels[value] ?? (value || '-')
}

export function backfillStatusTone(value: string) {
  if (value === 'created') return 'success' as const
  if (value === 'pending') return 'warning' as const
  if (value === 'failed' || value === 'invalid') return 'danger' as const
  return 'neutral' as const
}
