import { isReportTimeDefault, previewReportTimeDefault, reportTimeDefaultOptions } from '../../timeDefaults.js'
import type { ReportInputField, ReportQueryOperator, ReportQueryValue } from '../../types'

export type ComputedReportValue = Extract<ReportQueryValue, { kind: 'TIME' | 'TIME_RANGE' | 'COLUMN_MAX' }>
export const reportMonthRanges = [{ value: 'CURRENT_MONTH', label: '当月' }, { value: 'PREVIOUS_MONTH', label: '上月' }, { value: 'NEXT_MONTH', label: '下月' }] as const
export const isComputedReportValue = (value: ReportQueryValue | undefined): value is ComputedReportValue => value?.kind === 'TIME' || value?.kind === 'TIME_RANGE' || value?.kind === 'COLUMN_MAX'
export const supportsComputedReportValue = (type?: string) => !type || ['string', 'integer', 'decimal', 'date', 'datetime', 'enum'].includes(type)
export const supportsReportTimeValue = (type?: string) => ['integer', 'decimal', 'date', 'datetime'].includes(type ?? '')
export function reportTimeOperator(operators: ReportQueryOperator[], current?: ReportQueryOperator | '') {
  const scalar = operators.filter((operator) => ['EQ', 'NE', 'GT', 'GTE', 'LT', 'LTE'].includes(operator))
  return current && scalar.includes(current) ? current : scalar.includes('GTE') ? 'GTE' as const : scalar[0]
}

export function reportTimeField(type?: string): Pick<ReportInputField, 'type' | 'control' | 'format'> {
  if (type === 'date') return { type: 'str', control: 'DATE', format: 'YYYY-MM-DD' }
  if (type === 'datetime') return { type: 'str', control: 'DATETIME', format: 'YYYY-MM-DD HH:mm:ss' }
  return { type: type === 'integer' || type === 'decimal' ? 'number' : 'str', control: 'TEXT' }
}

export function computedReportValueError(value: ComputedReportValue, operator: ReportQueryOperator, type?: string): string {
  if (!supportsComputedReportValue(type)) return '该字段类型不支持动态时间或最大值。'
  if (value.kind === 'COLUMN_MAX') return operator === 'EQ' ? '' : '本列最大值只能使用“等于”。'
  if (!supportsReportTimeValue(type)) return '动态时间仅支持数值日期、DATE 和 DATETIME 字段。'
  if (value.kind === 'TIME_RANGE') return operator === 'BETWEEN' && reportMonthRanges.some((item) => item.value === value.code) ? '' : '月份范围必须使用“区间”。'
  if (!['EQ', 'NE', 'GT', 'GTE', 'LT', 'LTE'].includes(operator) || !isReportTimeDefault(value.code)) return '动态时间必须使用单值比较。'
  return reportTimeDefaultOptions(reportTimeField(type)).some((item) => item.value === value.code) ? '' : '动态时间与字段类型不匹配。'
}

export function computedReportValuePreview(value: ComputedReportValue, type?: string, now = new Date()) {
  if (value.kind === 'COLUMN_MAX') return '先应用其他筛选，再取本列最大值；并列最大值全部保留。每次查询最多一项，只能用于 AND 条件。'
  const field = reportTimeField(type)
  if (value.kind === 'TIME') return `当前预览：${previewReportTimeDefault({ ...field, defaultTime: value.code }, now)}${value.code === 'MONTH_END' || value.code === 'YEAR_END' ? '（日期上限含末日全天）' : ''}；查询时按北京时间更新。`
  const beijing = new Date(now.getTime() + 8 * 60 * 60 * 1000)
  const offset = value.code === 'PREVIOUS_MONTH' ? -1 : value.code === 'NEXT_MONTH' ? 1 : 0
  const month = new Date(Date.UTC(beijing.getUTCFullYear(), beijing.getUTCMonth() + offset, 1))
  const start = previewReportTimeDefault({ ...field, defaultTime: 'MONTH_START' }, month)
  const end = previewReportTimeDefault({ ...field, defaultTime: 'MONTH_END' }, month)
  return `当前预览：${start} 至 ${end}${type === 'date' || type === 'datetime' ? '（含末日全天）' : '（含首尾）'}；查询时按北京时间更新。`
}
