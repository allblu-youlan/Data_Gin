import type { ReportAllowedFilter, ReportQueryCondition, ReportQueryConditionGroup, ReportQueryInputs, ReportQueryOperator, ReportQueryValue, ReportRunContract } from '../../types'
import { computedReportValueError, isComputedReportValue } from './computedValues.js'

export const runtimeOperatorLabels: Record<ReportQueryOperator, string> = { EQ: '等于', NE: '不等于', GT: '大于', GTE: '大于等于', LT: '小于', LTE: '小于等于', BETWEEN: '区间', IN: '属于集合', NOT_IN: '不属于集合', PREFIX: '开头为', CONTAINS: '包含', IS_NULL: '为空', IS_NOT_NULL: '不为空' }
export const runtimeFilterKey = (field: { aliasId: string; columnId: string }) => `${field.aliasId}:${field.columnId}`
export const runtimeNoValue = (operator: string) => operator === 'IS_NULL' || operator === 'IS_NOT_NULL'
export const runtimeCollection = (operator: string) => operator === 'IN' || operator === 'NOT_IN'
const blank = (value: unknown) => value === undefined || value === null || typeof value === 'string' && !value.trim()

export function readRuntimeFilter(value: ReportQueryInputs, field: ReportAllowedFilter): ReportQueryCondition | undefined {
  return value.where?.items.find((item): item is ReportQueryCondition => !('logic' in item) && runtimeFilterKey(item) === runtimeFilterKey(field))
}

export function isSimpleRuntimeWhere(contract: Pick<ReportRunContract, 'allowedFilters'>, value: ReportQueryInputs): boolean {
  if (!value.where) return true
  if (value.where.logic !== 'AND') return false
  const seen = new Set<string>()
  return value.where.items.every((item) => {
    if ('logic' in item) return false
    const key = runtimeFilterKey(item)
    const allowed = contract.allowedFilters?.find((field) => runtimeFilterKey(field) === key)
    if (seen.has(key) || !allowed?.operators.includes(item.operator) || item.value && item.value.kind !== 'CONSTANT' && !isComputedReportValue(item.value)) return false
    if (isComputedReportValue(item.value) && computedReportValueError(item.value, item.operator, allowed.logicalType)) return false
    if (!runtimeNoValue(item.operator) && !runtimeCollection(item.operator) && item.value?.kind === 'CONSTANT') {
      const raw = item.value.value
      const values = item.operator === 'BETWEEN' ? Array.isArray(raw) ? raw : [raw] : [raw]
      if (!values.every((entry) => nativeDateCompatible(entry, allowed.logicalType))) return false
    }
    seen.add(key)
    return true
  })
}

export function updateRuntimeFilter(value: ReportQueryInputs, field: ReportAllowedFilter, operator: ReportQueryOperator | '', raw?: unknown, source?: ReportQueryValue): ReportQueryInputs {
  if (operator && !field.operators.includes(operator)) throw new Error('该字段不允许使用此筛选方式。')
  const items = (value.where?.items ?? []).filter((item) => 'logic' in item || runtimeFilterKey(item) !== runtimeFilterKey(field))
  const condition: ReportQueryCondition = { aliasId: field.aliasId, columnId: field.columnId, operator: operator || field.operators[0] }
  if (operator && !runtimeNoValue(operator)) condition.value = source ?? { kind: 'CONSTANT', value: raw ?? '' }
  return { ...value, where: { logic: 'AND', items: operator ? [...items, condition] : items } }
}

function normalizeGroup(group: ReportQueryConditionGroup): ReportQueryConditionGroup {
  const items = group.items.flatMap((item): Array<ReportQueryCondition | ReportQueryConditionGroup> => {
    if ('logic' in item) { const next = normalizeGroup(item); return next.items.length ? [next] : [] }
    if (runtimeNoValue(item.operator) || item.value?.kind !== 'CONSTANT') return [item]
    let raw = item.value.value
    if (Array.isArray(raw)) {
      if (raw.every(blank)) return []
      raw = raw.map((entry) => typeof entry === 'string' ? entry.trim() : entry)
      if (runtimeCollection(item.operator)) raw = (raw as unknown[]).filter((entry) => !blank(entry))
    } else {
      if (blank(raw)) return []
      if (typeof raw === 'string') raw = raw.trim()
    }
    return [{ ...item, value: { kind: 'CONSTANT', value: raw } }]
  })
  return { ...group, items }
}

export function normalizeRuntimeQueryInputs(value: ReportQueryInputs): ReportQueryInputs {
  return { ...value, ...(value.where ? { where: normalizeGroup(value.where) } : {}) }
}

export function runtimeDateInput(value: unknown, logicalType?: string): string {
  const text = String(value ?? '')
  if (logicalType === 'date' && /^\d{8}$/.test(text)) return `${text.slice(0, 4)}-${text.slice(4, 6)}-${text.slice(6, 8)}`
  return logicalType === 'datetime' ? text.replace(' ', 'T') : text
}

function nativeDateCompatible(value: unknown, logicalType?: string): boolean {
  if (blank(value) || logicalType !== 'date' && logicalType !== 'datetime') return true
  const text = String(value)
  return logicalType === 'date' ? /^(?:\d{4}-\d{2}-\d{2}|\d{8})$/.test(text) : /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(text)
}

export function runtimeDateValue(value: string, logicalType?: string): string {
  return value && logicalType === 'datetime' ? `${value.replace('T', ' ')}${value.length === 16 ? ':00' : ''}` : value
}

function validScalar(value: unknown, type?: string): string {
  if (typeof value !== 'string' && typeof value !== 'number') return '请填写有效的筛选值。'
  const text = String(value)
  if (type === 'integer' || type === 'decimal') {
    const pattern = type === 'integer' ? /^[+-]?\d+$/ : /^[+-]?(?:\d+(?:\.\d+)?|\.\d+)$/
    return pattern.test(text) && text.length <= 80 ? '' : type === 'integer' ? '请输入有效整数。' : '请输入有效数字。'
  }
  if (type === 'date' || type === 'datetime') {
    const pattern = /^(?:\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2}:\d{2}(?:\.\d+)?|T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))?|\d{8}(?:\d{6})?)$/
    if (!pattern.test(text)) return '请填写有效日期。'
    const parts = text.match(/^(\d{4})-?(\d{2})-?(\d{2})/)
    if (parts) {
      const calendar = new Date(0)
      calendar.setUTCFullYear(Number(parts[1]), Number(parts[2]) - 1, Number(parts[3]))
      if (calendar.getUTCFullYear() !== Number(parts[1]) || calendar.getUTCMonth() !== Number(parts[2]) - 1 || calendar.getUTCDate() !== Number(parts[3])) return '请填写有效日期。'
    }
    const time = text.match(/[T ](\d{2}):(\d{2}):(\d{2})/) ?? text.match(/^\d{8}(\d{2})(\d{2})(\d{2})$/)
    if (time && (Number(time[1]) > 23 || Number(time[2]) > 59 || Number(time[3]) > 59)) return '请填写有效日期。'
  }
  return text.length <= 4000 ? '' : '筛选值最多为 4000 个字符。'
}

export function validateRuntimeQueryInputs(contract: Pick<ReportRunContract, 'allowedFilters' | 'allowedSortFields'>, value: ReportQueryInputs): string {
  let count = 0
  let maxCount = 0
  function validate(group: ReportQueryConditionGroup, depth: number, andPath = true): string {
    if (depth >= 3 || !['AND', 'OR'].includes(group.logic)) return '筛选分组最多为 3 层。'
    for (const item of group.items) {
      if ('logic' in item) { const error = validate(item, depth + 1, andPath && group.logic === 'AND'); if (error) return error; continue }
      if (++count > 20) return '每次查询最多设置 20 个筛选条件。'
      const field = contract.allowedFilters?.find((entry) => runtimeFilterKey(entry) === runtimeFilterKey(item))
      if (!field || !field.operators.includes(item.operator)) return '筛选字段或操作符未获当前报表允许，请重新选择。'
      const label = field.label || `字段 #${field.columnId}`
      if (runtimeNoValue(item.operator)) { if (item.value !== undefined) return `${label}：空值筛选无需填写值。`; continue }
      if (isComputedReportValue(item.value)) {
        const error = computedReportValueError(item.value, item.operator, field.logicalType)
        if (error) return `${label}：${error}`
        if (item.value.kind === 'COLUMN_MAX') {
          if (!andPath || group.logic !== 'AND') return '本列最大值只能用于 AND 条件。'
          if (++maxCount > 1) return '每次查询最多设置一项本列最大值筛选。'
        }
        continue
      }
      if (item.value?.kind !== 'CONSTANT') return `${label}：请直接填写筛选值。`
      const raw = item.value.value
      const collection = item.operator === 'BETWEEN' || runtimeCollection(item.operator)
      if (collection && !Array.isArray(raw)) return `${label}：请填写${item.operator === 'BETWEEN' ? '区间起止值' : '用逗号分隔的集合值'}。`
      const values = collection ? raw as unknown[] : [raw]
      if (item.operator === 'BETWEEN' && (values.length !== 2 || values.some(blank))) return `${label}：请完整填写区间起止值。`
      if (runtimeCollection(item.operator) && (values.length === 0 || values.length > 100)) return `${label}：集合需包含 1 至 100 个值。`
      for (const entry of values) { const error = validScalar(entry, field.logicalType); if (error) return `${label}：${error}` }
    }
    return ''
  }
  const normalized = normalizeRuntimeQueryInputs(value)
  if (normalized.where) { const error = validate(normalized.where, 0); if (error) return error }
  if ((value.orderBy?.length ?? 0) > 3) return '每次查询最多设置 3 个排序字段。'
  if (value.orderBy?.some((order) => !contract.allowedSortFields?.some((field) => runtimeFilterKey(field) === runtimeFilterKey(order)) || !['ASC', 'DESC'].includes(order.direction))) return '排序字段未获当前报表允许，请重新选择。'
  return ''
}
