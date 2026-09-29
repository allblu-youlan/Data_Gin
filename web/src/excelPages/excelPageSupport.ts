import { apiURL as buildApiURL } from '../apiURL'
import {
  cloneExcelEmptyCellFills,
  migrateExcelImportWriteMappings,
  migrateExcelMatchSteps,
  type ExcelEmptyCellFillConfig,
  type ExcelMatchFilterConfig,
  type ExcelImportWriteMappingConfig,
  type ExcelMatchModel,
  type ExcelMatchStepConfig,
} from '../excelMatchConfig'
import type { ClientResponse } from '../api/client'

export type ExcelMatchJob = { id: number; source_file_name: string; config_json?: string; status: string; total_rows: number; processed_rows: number; filtered_rows: number; matched_rows: number; unmatched_rows: number; error_message?: string; started_at: string | null; finished_at: string | null; expires_at: string | null; result_url?: string; can_download?: boolean; download_message?: string; operation?: string; created_at: number }
export type ExcelMatchJobLog = { id: number; job_id: number; level: string; message: string; detail_json: string; created_at: number }
export type ExcelDialogMode = 'export' | 'import' | 'clear' | 'query'
export type ExcelUploadSlot = 'export' | 'import' | 'clear'
export type ExcelUploadSession = { uploadId: string; fileName: string; totalChunks: number; uploadedChunks: number; complete: boolean; expiresAt: string }
export type ExcelUploadRef = { uploadId: string; fileName: string; size: number; lastModified: number; totalChunks: number }
export type ExcelExportColumnFormat = { column: string; format: string }
export type ExcelExportSchemeConfig = { sheetName: string; steps: ExcelMatchStepConfig[]; emptyCellFills: ExcelEmptyCellFillConfig[]; exportColumnFormats: string; batchSize: string }
export type ExcelImportSchemeConfig = { sheetName: string; tableName: string; dbMatchField: string; matchExcelColumn: string; writeMappings: ExcelImportWriteMappingConfig[]; batchSize: string }
export type ExcelMatchSchemeConfig = { operation?: string; sheetName?: string; filters?: Array<Partial<ExcelMatchFilterConfig>>; matchExcelColumn?: string; dbTemplate?: string; dbMatchField?: string; dbValueField?: string; tableName?: string; dbWriteField?: string; writeExcelColumn?: string; writeMappings?: Array<Partial<ExcelImportWriteMappingConfig>>; outputColumnName?: string; steps?: ExcelMatchStepConfig[]; emptyCellFills?: Array<Partial<ExcelEmptyCellFillConfig>>; exportColumnFormats?: ExcelExportColumnFormat[]; batchSize?: number; dryRun?: boolean; confirmWrite?: boolean }
export type ExcelMatchScheme = { id: number; name: string; operation: string; config: ExcelMatchSchemeConfig; config_json: string; created_at: number; updated_at: number }
export type PendingSchemeSave = { operation: 'export_match' | 'import_update'; config: unknown; name: string; overwriteConfirmed: boolean }
export type ExcelMatchPreviewStats = { TotalRows?: number; ProcessedRows?: number; FilteredRows?: number; MatchedRows?: number; UnmatchedRows?: number }
export type ExcelMatchPreviewSample = { rowNumber: number; matchKey: string; matchedValue: string; status: string; reason: string; values: Record<string, string>; stepResults?: Array<{ stepIndex: number; stepName: string; matchKey: string; matchedValue: string; status: string; reason: string }> }
export type ExcelMatchPreviewResult = { stats: ExcelMatchPreviewStats; scanLimit: number; sampleLimit: number; truncated: boolean; samples: ExcelMatchPreviewSample[] }

export const bojunMatchFieldOptions = [
  { value: 'docno', label: '订单号 docno' },
  { value: 'otherdocno', label: '外部单号 otherdocno' },
  { value: 'o2o_so_docno', label: '线上订单号 o2o_so_docno' },
  { value: 'related_normal_docno', label: '关联原单 related_normal_docno' },
  { value: 'matched_docno', label: '匹配单号 matched_docno' },
]
export const excelChunkSize = 4 * 1024 * 1024
export const excelJobPollMaxAttempts = 60
export const excelMatchFilterOperatorOptions = [
  { value: 'eq', label: '等于' }, { value: 'neq', label: '不等于' }, { value: 'contains', label: '包含' },
  { value: 'not_contains', label: '不包含' }, { value: 'starts_with', label: '开头是' }, { value: 'ends_with', label: '结尾是' },
  { value: 'empty', label: '为空' }, { value: 'not_empty', label: '不为空' },
]
export const defaultExcelExportScheme: ExcelExportSchemeConfig = {
  sheetName: 'Sheet1',
  steps: [{ name: '匹配伯俊门店', filters: [{ column: '店铺', op: 'eq', value: '幼岚-有赞' }], matchMode: 'field', tableName: 'bojun_retail_orders', matchExcelColumn: '原始线上订单号', dbMatchField: 'matched_docno', dbValueField: 'c_store_name', outputColumnName: '线下店名称', conditionOp: 'contains', containsValue: '', writeValue: '', specExcelColumn: '', priceExcelColumn: '', qtyExcelColumn: '' }],
  emptyCellFills: [], exportColumnFormats: '', batchSize: '1000',
}
export const defaultExcelImportScheme: ExcelImportSchemeConfig = { sheetName: 'Sheet1', tableName: 'bojun_retail_orders', dbMatchField: 'docno', matchExcelColumn: '外部订单编号', writeMappings: [{ dbWriteField: 'matched_docno', writeExcelColumn: '订单号' }], batchSize: '1000' }

export function excelJobProgressPercent(job: ExcelMatchJob) { return job.total_rows <= 0 ? 0 : Math.min(100, Math.max(0, Math.round(job.processed_rows / job.total_rows * 100))) }
export function readList<T>(result: ClientResponse, key: string): T[] { const value = readDataField(result.data, key); return Array.isArray(value) ? value as T[] : [] }
export function readObject<T>(result: ClientResponse, key: string): T | null { const value = readDataField(result.data, key); return value && typeof value === 'object' ? value as T : null }
export function readDataField(data: unknown, key: string) { if (!data || typeof data !== 'object') return undefined; return (data as { data?: Record<string, unknown> }).data?.[key] }
export function filterSensitiveExcelModels(models: ExcelMatchModel[]): ExcelMatchModel[] { return models.flatMap((model) => !model || typeof model.name !== 'string' || typeof model.modelName !== 'string' || typeof model.tableName !== 'string' || typeof model.description !== 'string' || typeof model.mapping !== 'string' || !Array.isArray(model.fields) || isSensitiveExcelCatalogValue(model.name, model.modelName, model.tableName, model.description) ? [] : [{ ...model, fields: model.fields.filter((field) => field && typeof field.name === 'string' && typeof field.modelField === 'string' && typeof field.columnName === 'string' && typeof field.description === 'string' && !isSensitiveExcelCatalogValue(field.name, field.modelField, field.columnName, field.description)) }]) }
export function isSensitiveExcelCatalogValue(...values: string[]) { return values.some((value) => /(?:access[_-]?token|refresh[_-]?token|authorization|api[_-]?key|secret|password|credential)/i.test(value)) }
export function formValue(form: FormData, key: string) { const value = form.get(key); return typeof value === 'string' ? value : '' }
export function parseExportColumnFormats(raw: string): ExcelExportColumnFormat[] { return raw.split(/\r?\n/).map((line) => line.trim()).filter(Boolean).map((line) => { const separator = line.includes('=') ? '=' : ':'; const [column = '', format = ''] = line.split(separator); return { column: column.trim(), format: format.trim().toLowerCase() } }).filter((item) => item.column && item.format) }
export function exportColumnFormatsText(formats: ExcelExportColumnFormat[] | undefined) { return Array.isArray(formats) ? formats.filter((item) => item.column && item.format).map((item) => `${item.column}=${item.format}`).join('\n') : '' }
export function sameExcelFile(file: File, ref: ExcelUploadRef) { return file.name === ref.fileName && file.size === ref.size && file.lastModified === ref.lastModified }
export function exportSchemeDefaults(config: ExcelMatchSchemeConfig): ExcelExportSchemeConfig { const steps = migrateExcelMatchSteps(config, defaultExcelExportScheme.steps[0]); return { sheetName: config.sheetName || defaultExcelExportScheme.sheetName, steps, emptyCellFills: cloneExcelEmptyCellFills(config.emptyCellFills), exportColumnFormats: exportColumnFormatsText(config.exportColumnFormats) || defaultExcelExportScheme.exportColumnFormats, batchSize: config.batchSize ? String(config.batchSize) : defaultExcelExportScheme.batchSize } }
export function importSchemeDefaults(config: ExcelMatchSchemeConfig): ExcelImportSchemeConfig { return { sheetName: config.sheetName || defaultExcelImportScheme.sheetName, tableName: config.tableName || defaultExcelImportScheme.tableName, dbMatchField: config.dbMatchField || defaultExcelImportScheme.dbMatchField, matchExcelColumn: config.matchExcelColumn || defaultExcelImportScheme.matchExcelColumn, writeMappings: migrateExcelImportWriteMappings(config, defaultExcelImportScheme.writeMappings[0]), batchSize: config.batchSize ? String(config.batchSize) : defaultExcelImportScheme.batchSize } }
export function excelJobStatusLabel(value: string) { return ({ pending: '等待处理', running: '处理中', success: '成功', failed: '失败', expired: '已过期' } as Record<string, string>)[value] ?? (value || '-') }
export function parseMaybeJson(value: unknown) { if (!value) return null; if (typeof value === 'object') return value; if (typeof value !== 'string') return null; try { return JSON.parse(value) as unknown } catch { return null } }
export function excelJobOperation(job: ExcelMatchJob) { if (job.operation) return job.operation; const config = parseMaybeJson(job.config_json ?? ''); return config && typeof config === 'object' && typeof (config as Record<string, unknown>).operation === 'string' ? String((config as Record<string, unknown>).operation) : '' }
export function excelJobOperationLabel(value: string) { return ({ export_match: '匹配导出', import_update: '匹配导入', clear_matched_docno: '退回未匹配' } as Record<string, string>)[value] ?? (value || '-') }
export function canDownloadExcelJob(job: ExcelMatchJob) { return typeof job.can_download === 'boolean' ? job.can_download : job.status === 'success' && excelJobOperation(job) === 'export_match' && Boolean(job.result_url) }
export function isExcelJobActive(job: ExcelMatchJob | null | undefined) { return Boolean(job && !['success', 'failed', 'expired'].includes(job.status)) }
export function replaceExcelJobHistoryItem(jobs: ExcelMatchJob[], nextJob: ExcelMatchJob) { const index = jobs.findIndex((item) => item.id === nextJob.id); if (index === -1) return jobs; const next = [...jobs]; next[index] = nextJob; return next }
export function excelPreviewStat(stats: ExcelMatchPreviewStats, key: keyof ExcelMatchPreviewStats) { return typeof stats[key] === 'number' ? stats[key] as number : 0 }
export function excelPreviewStatusLabel(value: string) { return ({ matched: '已匹配', unmatched: '未匹配', skipped: '已跳过' } as Record<string, string>)[value] ?? (value || '-') }
export function excelLogLevelLabel(value: string) { return ({ info: '信息', warn: '警告', error: '错误' } as Record<string, string>)[value] ?? (value || '-') }
export function formatUnixTime(value: number) { return value ? formatDate(new Date(value * 1000).toISOString()) : '-' }
export function formatDate(value: string | null) { if (!value) return '-'; const normalized = value.includes('T') ? value : `${value.replace(' ', 'T')}+08:00`; const date = new Date(normalized); if (Number.isNaN(date.getTime())) return value; return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date).replace(/\//g, '-') }
export function compactText(value: string) { const text = (value || '').replace(/\s+/g, ' ').trim(); return text.length <= 120 ? text : `${text.slice(0, 120)}...` }

export function submitExcelDownloadForm(jobID: number, token: string, baseURL = import.meta.env.VITE_API_BASE_URL ?? '') {
  if (!token) throw new Error('登录状态已失效，请重新登录后下载')
  const frameName = `excel-download-frame-${jobID}`
  let iframe = document.querySelector<HTMLIFrameElement>(`iframe[name="${frameName}"]`)
  if (!iframe) { iframe = document.createElement('iframe'); iframe.name = frameName; iframe.style.display = 'none'; document.body.appendChild(iframe) }
  const form = document.createElement('form'); form.method = 'POST'; form.action = buildApiURL(`/v1/excel-match-jobs/${jobID}/download`, baseURL); form.target = frameName; form.style.display = 'none'
  const tokenInput = document.createElement('input'); tokenInput.type = 'hidden'; tokenInput.name = 'token'; tokenInput.value = token; form.appendChild(tokenInput)
  document.body.appendChild(form); form.submit(); form.remove(); window.setTimeout(() => iframe?.remove(), 5 * 60 * 1000)
}
