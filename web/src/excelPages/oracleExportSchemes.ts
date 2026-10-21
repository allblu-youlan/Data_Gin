import type { ExcelPageClient } from './useExcelUploads'
import { oracleExportDateInputValue, oracleExportFilterInputType, oracleExportFilterOperators, type OracleExportColumnDraft, type OracleExportFilter, type OracleExportFilterDraft } from './oracleExportConfig.js'

export type OracleExportConfig = { owner: string; tableName: string; columns: Array<{ field: string; title: string }>; filters: OracleExportFilter[] }
export type OracleExportScheme = { id: number; name: string; operation: 'oracle_export'; config: OracleExportConfig; updatedAt: number }
const schemePath = '/v1/excel-match-jobs/oracle-export-schemes'
const quiet = { showResult: false, silentLoading: true }

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('导出配置响应格式不完整，请重试。')
  return value as Record<string, unknown>
}

function data(payload: unknown) {
  const root = record(payload)
  return root.data === undefined ? root : record(root.data)
}

function parseScheme(value: unknown): OracleExportScheme {
  const item = record(value)
  const config = record(item.config)
  if (!Number.isInteger(item.id) || Number(item.id) <= 0 || typeof item.name !== 'string' || !item.name || item.operation !== 'oracle_export' || typeof config.owner !== 'string' || !config.owner || typeof config.tableName !== 'string' || !config.tableName || !Array.isArray(config.columns) || !Array.isArray(config.filters)) throw new Error('导出配置内容不完整，请重试。')
  const columns = config.columns.map((value) => {
    const column = record(value)
    if (typeof column.field !== 'string' || !column.field || typeof column.title !== 'string') throw new Error('导出配置字段格式不完整。')
    return { field: column.field, title: column.title }
  })
  const filters = config.filters.map((value) => {
    const filter = record(value)
    const operator = oracleExportFilterOperators.find((item) => item.value === filter.op)
    if (typeof filter.field !== 'string' || !filter.field || !operator || typeof filter.value !== 'string') throw new Error('导出配置筛选条件格式不完整。')
    return { field: filter.field, op: operator.value, value: filter.value }
  })
  return { id: Number(item.id), name: item.name, operation: 'oracle_export', config: { owner: config.owner, tableName: config.tableName, columns, filters }, updatedAt: typeof item.updated_at === 'number' ? item.updated_at : 0 }
}

export function parseOracleExportSchemes(payload: unknown) {
  const items = data(payload).schemes
  if (!Array.isArray(items)) throw new Error('导出配置列表格式不完整，请重试。')
  return items.map(parseScheme)
}

export async function getOracleExportSchemes(client: ExcelPageClient, signal?: AbortSignal) {
  const response = await client(schemePath, { method: 'GET', signal, ...quiet })
  if (!response.ok) throw new Error(response.error?.message || '导出配置加载失败，请重试。')
  return parseOracleExportSchemes(response.data)
}

export async function saveOracleExportScheme(client: ExcelPageClient, name: string, config: OracleExportConfig) {
  const response = await client(schemePath, { method: 'POST', body: { name: name.trim(), config }, retry: false, ...quiet })
  if (!response.ok) throw new Error(response.error?.message || '导出配置保存失败，请重试。')
  return parseScheme(data(response.data).scheme)
}

export async function deleteOracleExportScheme(client: ExcelPageClient, id: number) {
  const response = await client(`${schemePath}/${id}`, { method: 'DELETE', retry: false, ...quiet })
  if (!response.ok) throw new Error(response.error?.message || '导出配置删除失败，请重试。')
}

export function restoreOracleExportConfig(config: OracleExportConfig, metadata: OracleExportColumnDraft[]) {
  const selected = new Map(config.columns.map((column) => [column.field, column.title]))
  const available = new Set(metadata.map((column) => column.name))
  const missingColumns = config.columns.filter((column) => !available.has(column.field)).map((column) => column.field)
  const missingFilters = config.filters.filter((filter) => !available.has(filter.field)).map((filter) => filter.field)
  const columns = metadata.map((column) => ({ ...column, selected: config.columns.length === 0 || selected.has(column.name), title: selected.get(column.name)?.trim() || column.name }))
  const filters: OracleExportFilterDraft[] = config.filters.map((filter, index) => ({ ...filter, id: index + 1, value: oracleExportFilterInputType(metadata.find((column) => column.name === filter.field)?.dataType ?? '') === 'datetime-local' ? oracleExportDateInputValue(filter.value) : filter.value }))
  const issues = [...(missingColumns.length ? [`导出字段已失效：${missingColumns.join('、')}`] : []), ...(missingFilters.length ? [`筛选字段已失效：${[...new Set(missingFilters)].join('、')}`] : [])]
  return { columns, filters, issues }
}
