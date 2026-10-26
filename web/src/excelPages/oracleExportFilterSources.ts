import { computedReportValueError, type ComputedReportValue } from '../reportCenter/components/ReportQueryDesigner/computedValues.js'
import { isReportTimeDefault } from '../reportCenter/timeDefaults.js'
import type { ReportQueryOperator } from '../reportCenter/types'
import type { OracleExportFilter } from './oracleExportConfig'

export function parseOracleExportFilterSource(value: unknown): ComputedReportValue {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('导出筛选的值来源无效。')
  const source = value as Record<string, unknown>
  if (source.kind === 'COLUMN_MAX' && source.code === undefined) return { kind: 'COLUMN_MAX' }
  if (source.kind === 'TIME' && isReportTimeDefault(source.code)) return { kind: 'TIME', code: source.code }
  if (source.kind === 'TIME_RANGE' && (source.code === 'CURRENT_MONTH' || source.code === 'PREVIOUS_MONTH' || source.code === 'NEXT_MONTH')) return { kind: 'TIME_RANGE', code: source.code }
  throw new Error('导出筛选的动态时间或最大值来源无效。')
}

export function oracleExportFilterLogicalType(dataType: string) {
  if (/^(DATE|TIMESTAMP)(?:\b|\()/i.test(dataType.trim())) return 'datetime'
  if (/^(NUMBER|NUMERIC|FLOAT|BINARY_FLOAT|BINARY_DOUBLE|INTEGER|DECIMAL)(?:\b|\()/i.test(dataType.trim())) return 'decimal'
  return 'string'
}

export function oracleExportComputedFilterError(filter: OracleExportFilter, dataType: string) {
  if (!filter.valueSource) return filter.op === 'between' ? '月份区间需要选择“动态月份范围”值来源。' : ''
  let source: ComputedReportValue
  try { source = parseOracleExportFilterSource(filter.valueSource) } catch (cause) { return cause instanceof Error ? cause.message : '导出筛选的值来源无效。' }
  if (source.kind === 'COLUMN_MAX' && /^(CLOB|NCLOB|BLOB|LONG|RAW)(?:\b|\()/i.test(dataType.trim())) return '该字段类型不支持本列最大值。'
  const operators: Partial<Record<OracleExportFilter['op'], ReportQueryOperator>> = { eq: 'EQ', neq: 'NE', gt: 'GT', gte: 'GTE', lt: 'LT', lte: 'LTE', between: 'BETWEEN', contains: 'CONTAINS', empty: 'IS_NULL', not_empty: 'IS_NOT_NULL' }
  const operator = operators[filter.op]
  const logicalType = filter.dateFormat && source.kind !== 'COLUMN_MAX' && oracleExportFilterLogicalType(dataType) === 'string' && /^(?:N?VARCHAR2?|N?CHAR)(?:\b|\()/i.test(dataType.trim()) ? 'decimal' : oracleExportFilterLogicalType(dataType)
  return operator ? computedReportValueError(source, operator, logicalType) : '不支持的比较方式。'
}
