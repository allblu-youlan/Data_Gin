import { formatShanghaiDateTime } from '../dateTime.js'
import type { DeliveryLog, PipelineRun, StepRun } from './types'

function publicText(value: unknown, maximumLength: number) {
  if (typeof value !== 'string') return ''
  const text = value.trim()
  return text.length <= maximumLength ? text : ''
}

function publicDateTime(value: unknown) {
  return value === null ? null : publicText(value, 32) || null
}

function publicNonNegativeInteger(value: unknown) {
  const number = Number(value)
  return Number.isSafeInteger(number) && number >= 0 ? number : -1
}

export function parsePipelineRun(value: unknown): PipelineRun | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const run = value as Record<string, unknown>
  const id = publicNonNegativeInteger(run.id)
  const sourceID = publicNonNegativeInteger(run.source_id)
  const destinationID = publicNonNegativeInteger(run.destination_id)
  const totalCount = publicNonNegativeInteger(run.total_count)
  const successCount = publicNonNegativeInteger(run.success_count)
  const failedCount = publicNonNegativeInteger(run.failed_count)
  const status = publicText(run.status, 24)
  const runType = publicText(run.run_type, 32)
  if (id <= 0 || sourceID < 0 || destinationID < 0 || totalCount < 0 || successCount < 0 || failedCount < 0
    || !['running', 'success', 'failed', 'partial_success'].includes(status)
    || !['fetch', 'ingest', 'transform', 'delivery'].includes(runType)) return null
  return {
    id,
    trace_id: publicText(run.trace_id, 64),
    run_type: runType,
    trigger_type: publicText(run.trigger_type, 50),
    status,
    total_count: totalCount,
    success_count: successCount,
    failed_count: failedCount,
    source_id: sourceID,
    destination_id: destinationID,
    started_at: publicDateTime(run.started_at),
    finished_at: publicDateTime(run.finished_at),
  }
}

export function parseStepRun(value: unknown): StepRun | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const step = value as Record<string, unknown>
  const id = publicNonNegativeInteger(step.id)
  const runID = publicNonNegativeInteger(step.run_id)
  const pipelineID = publicNonNegativeInteger(step.pipeline_id)
  const stepID = publicNonNegativeInteger(step.step_id)
  const status = publicText(step.status, 24)
  if (id <= 0 || runID <= 0 || pipelineID < 0 || stepID < 0 || !['running', 'success', 'failed', 'skipped'].includes(status)) return null
  return {
    id,
    run_id: runID,
    pipeline_id: pipelineID,
    step_id: stepID,
    step_code: publicText(step.step_code, 100),
    method_type: publicText(step.method_type, 50),
    status,
    input_json: publicText(step.input_json, 4096),
    output_json: publicText(step.output_json, 4096),
    generated_config_json: publicText(step.generated_config_json, 4096),
    error_message: publicText(step.error_message, 240),
    started_at: publicDateTime(step.started_at),
    finished_at: publicDateTime(step.finished_at),
  }
}

export function parseDeliveryLog(value: unknown): DeliveryLog | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const log = value as Record<string, unknown>
  const id = publicNonNegativeInteger(log.id)
  const runID = publicNonNegativeInteger(log.run_id)
  const destinationID = publicNonNegativeInteger(log.destination_id)
  const cleanRecordID = publicNonNegativeInteger(log.clean_record_id)
  const retryCount = publicNonNegativeInteger(log.retry_count)
  const httpStatus = publicNonNegativeInteger(log.http_status)
  if (id <= 0 || runID < 0 || destinationID < 0 || cleanRecordID < 0 || retryCount < 0 || httpStatus < 0 || typeof log.success !== 'boolean') return null
  return {
    id,
    trace_id: publicText(log.trace_id, 64),
    run_id: runID,
    source_code: publicText(log.source_code, 100),
    destination_code: publicText(log.destination_code, 100),
    destination_name: publicText(log.destination_name, 100),
    destination_id: destinationID,
    clean_record_id: cleanRecordID,
    business_key: publicText(log.business_key, 255),
    response_summary: publicText(log.response_summary, 240),
    http_status: httpStatus,
    success: log.success,
    error_message: publicText(log.error_message, 240),
    retry_count: retryCount,
    sent_at: publicDateTime(log.sent_at),
  }
}

export function pipelineRunStatusLabel(status: string) {
  const labels: Record<string, string> = { running: '运行中', success: '已完成', failed: '失败', partial_success: '部分成功' }
  return labels[status] ?? '未知'
}

export function stepRunStatusLabel(status: string) {
  const labels: Record<string, string> = { running: '运行中', success: '已完成', failed: '失败', skipped: '已跳过' }
  return labels[status] ?? '未知'
}

export function monitoringStatusTone(status: string) {
  if (status === 'success') return 'success' as const
  if (status === 'failed') return 'danger' as const
  if (status === 'running') return 'running' as const
  if (status === 'partial_success' || status === 'skipped') return 'warning' as const
  return 'neutral' as const
}

export function formatMonitoringDate(value: string | null) {
  return formatShanghaiDateTime(value, '-')
}

export function monitoringDurationLabel(startedAt: string | null, finishedAt: string | null) {
  if (!startedAt || !finishedAt) return '-'
  const milliseconds = Date.parse(finishedAt) - Date.parse(startedAt)
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '-'
  const seconds = Math.floor(milliseconds / 1000)
  if (seconds < 60) return `${seconds} 秒`
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
}

export function parseMonitoringJSON(value: string) {
  if (!value) return {}
  try {
    return JSON.parse(value) as unknown
  } catch {
    return value
  }
}

export function parseStepRunsResponse(payload: unknown): unknown[] | null {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return null
  const envelope = payload as Record<string, unknown>
  if (!envelope.data || typeof envelope.data !== 'object' || Array.isArray(envelope.data)) return null
  const stepRuns = (envelope.data as Record<string, unknown>).step_runs
  return Array.isArray(stepRuns) ? stepRuns : null
}
