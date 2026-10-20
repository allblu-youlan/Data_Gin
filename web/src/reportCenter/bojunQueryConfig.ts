import type { BojunColumn, BojunTable, ReportColumnSource, ReportExecutionPolicy, ReportQueryColumn, ReportQueryCondition, ReportQueryConditionGroup, ReportQueryOperator, ReportQueryOrder, ReportQueryRelation, ReportQuerySpec, ReportQueryValue } from './types'

export const reportQueryOperators: ReportQueryOperator[] = ['EQ', 'NE', 'GT', 'GTE', 'LT', 'LTE', 'BETWEEN', 'IN', 'NOT_IN', 'PREFIX', 'CONTAINS', 'IS_NULL', 'IS_NOT_NULL']
export const defaultReportExecutionPolicy: ReportExecutionPolicy = { exportPolicy: 'MANUAL', resultRetentionHours: 24, purgeAfterExport: false }
export function emptyReportQuerySpec(): ReportQuerySpec { return { version: 1, base: { tableId: '', aliasId: 'base' }, relations: [], columns: [], where: { logic: 'AND', items: [] }, orderBy: [], allowedFilters: [], allowedSortFields: [] } }
export function reportQueryColumnCode(fieldId: string) { return `f_${fieldId.replace(/-/g, '')}` }
export function newReportQueryColumn(source: ReportColumnSource, header: string): ReportQueryColumn { const fieldId = crypto.randomUUID(); return { fieldId, code: reportQueryColumnCode(fieldId), source, header, valueMode: 'DISPLAY', previewVisible: true, exportVisible: true } }
export function isQueryRecord(value: unknown): value is Record<string, unknown> { return Boolean(value) && typeof value === 'object' && !Array.isArray(value) }
export function queryText(value: unknown) { return typeof value === 'string' ? value : typeof value === 'number' && Number.isSafeInteger(value) ? String(value) : '' }
function array(value: unknown): unknown[] { return Array.isArray(value) ? value : [] }
export function queryRecord(value: unknown) { if (!isQueryRecord(value)) throw new Error('自定义查询配置不是对象。'); return value }
export function parseReportColumnSource(value: unknown): ReportColumnSource {
  const source = queryRecord(value)
  const columnId = queryText(source.columnId)
  const relationId = queryText(source.relationId)
  if (source.kind === 'BASE_COLUMN') return { kind: source.kind, columnId }
  if (source.kind === 'RELATION_DISPLAY') return { kind: source.kind, relationId }
  if (source.kind === 'RELATION_COLUMN') return { kind: source.kind, relationId, columnId }
  throw new Error('不支持的报表字段来源。')
}
export function parseReportQueryOperator(value: unknown): ReportQueryOperator { const operator = reportQueryOperators.find((item) => item === value); if (!operator) throw new Error('不支持的查询操作符。'); return operator }
function parseValue(value: unknown): ReportQueryValue {
  const item = queryRecord(value)
  if (item.kind === 'CONSTANT') return { kind: item.kind, value: item.value }
  if (item.kind === 'PARAMETER') return { kind: item.kind, code: queryText(item.code) }
  if (item.kind === 'FIELD') return { kind: item.kind, aliasId: queryText(item.aliasId), columnId: queryText(item.columnId) }
  throw new Error('不支持的查询条件值。')
}
export function parseReportQueryConditionGroup(value: unknown, depth = 0): ReportQueryConditionGroup {
  if (depth > 3) throw new Error('条件最多支持三层分组。')
  const group = queryRecord(value)
  if (group.logic !== 'AND' && group.logic !== 'OR') throw new Error('查询条件组必须选择 AND 或 OR。')
  return { logic: group.logic, items: array(group.items).map((value): ReportQueryCondition | ReportQueryConditionGroup => {
    const item = queryRecord(value)
    if ('logic' in item) return parseReportQueryConditionGroup(item, depth + 1)
    return { aliasId: queryText(item.aliasId), columnId: queryText(item.columnId), operator: parseReportQueryOperator(item.operator), ...(item.value === undefined ? {} : { value: parseValue(item.value) }) }
  }) }
}
export function parseReportQueryRelation(value: unknown): ReportQueryRelation {
  const item = queryRecord(value)
  return { id: queryText(item.id), name: queryText(item.name), fromAliasId: queryText(item.fromAliasId), targetTableId: queryText(item.targetTableId), keyBindings: array(item.keyBindings).map((value) => { const binding = queryRecord(value); return { sourceColumnId: queryText(binding.sourceColumnId), targetColumnId: queryText(binding.targetColumnId) } }), cardinality: item.cardinality === 'ONE_TO_ONE' ? 'ONE_TO_ONE' : 'MANY_TO_ONE', joinType: 'LEFT', displayColumnIds: array(item.displayColumnIds).map(queryText), detailColumnIds: array(item.detailColumnIds).map(queryText), matchConditions: item.matchConditions ? parseReportQueryConditionGroup(item.matchConditions) : { logic: 'AND', items: [] } }
}
export function parseReportQueryOrder(value: unknown): ReportQueryOrder { const item = queryRecord(value); return { aliasId: queryText(item.aliasId), columnId: queryText(item.columnId), direction: item.direction === 'DESC' ? 'DESC' : 'ASC' } }
export function parseReportQuerySpec(value: unknown): ReportQuerySpec {
  const item = queryRecord(value)
  if (item.version !== 1) throw new Error('不支持的查询配置版本。')
  const base = queryRecord(item.base)
  return {
    version: 1, base: { tableId: queryText(base.tableId), aliasId: queryText(base.aliasId) || 'base' }, relations: array(item.relations).map(parseReportQueryRelation),
    columns: array(item.columns).map((value): ReportQueryColumn => { const column = queryRecord(value); const fieldId = queryText(column.fieldId); return { fieldId, code: queryText(column.code) || reportQueryColumnCode(fieldId), source: parseReportColumnSource(column.source), header: queryText(column.header), valueMode: column.valueMode === 'RAW' ? 'RAW' : 'DISPLAY', ...(queryText(column.valueType) ? { valueType: queryText(column.valueType) } : {}), previewVisible: column.previewVisible !== false, exportVisible: column.exportVisible !== false } }),
    where: item.where ? parseReportQueryConditionGroup(item.where) : { logic: 'AND', items: [] }, orderBy: array(item.orderBy).map(parseReportQueryOrder),
    allowedFilters: array(item.allowedFilters).map((value) => { const filter = queryRecord(value); return { aliasId: queryText(filter.aliasId), columnId: queryText(filter.columnId), operators: array(filter.operators).map(parseReportQueryOperator), ...(filter.label ? { label: queryText(filter.label) } : {}) } }),
    allowedSortFields: array(item.allowedSortFields).map((value) => { const field = queryRecord(value); return { aliasId: queryText(field.aliasId), columnId: queryText(field.columnId) } }),
  }
}
export function parseReportExecutionPolicy(value: unknown): ReportExecutionPolicy {
  if (!isQueryRecord(value)) return { ...defaultReportExecutionPolicy }
  return { exportPolicy: value.exportPolicy === 'AUTO' ? 'AUTO' : 'MANUAL', resultRetentionHours: typeof value.resultRetentionHours === 'number' ? value.resultRetentionHours : 24, purgeAfterExport: value.purgeAfterExport === true }
}
export function queryColumnSourceLabel(source: ReportColumnSource) { return source.kind === 'BASE_COLUMN' ? `主表 · 字段 #${source.columnId}` : source.kind === 'RELATION_DISPLAY' ? `${source.relationId} · 默认显示` : `${source.relationId} · 字段 #${source.columnId}` }

export function isBojunColumnAllowed(table: BojunTable | undefined, column: BojunColumn, detail = false) {
  if (!table || !column.isPhysical || !column.isSelectable) return false
  const allowed = detail ? table.config?.detailColumns ?? table.config?.selectableColumns : table.config?.selectableColumns
  return allowed === null || allowed === undefined || allowed.includes(String(column.id))
}
