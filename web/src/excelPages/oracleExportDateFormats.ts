import { previewReportTimeDefault } from '../reportCenter/timeDefaults.js'
import type { ComputedReportValue } from '../reportCenter/components/ReportQueryDesigner/computedValues'

export const oracleExportDateFormats = ['yyyyMMdd', 'yyyy-MM-dd', 'yyyy/MM/dd', 'yyyy.MM.dd', 'yyyy-MM-dd HH:mm:ss', 'yyyy/MM/dd HH:mm:ss'] as const
export type OracleExportDateFormat = typeof oracleExportDateFormats[number]
export function isOracleExportDateFormat(value: unknown): value is OracleExportDateFormat {
  return oracleExportDateFormats.some((format) => format === value)
}

export function formatOracleExportDate(value: string, format: OracleExportDateFormat) {
  const match = /^(\d{4})[-/.]?(\d{2})[-/.]?(\d{2})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?$/.exec(value.trim())
  if (!match) return value
  const [, yyyy, MM, dd, HH = '00', mm = '00', ss = '00'] = match
  return format.replace('yyyy', yyyy).replace('MM', MM).replace('dd', dd).replace('HH', HH).replace('mm', mm).replace('ss', ss)
}

export function normalizeOracleExportDate(value: string, format: OracleExportDateFormat) {
  const trimmed = value.trim()
  const canonical = formatOracleExportDate(trimmed, 'yyyy-MM-dd HH:mm:ss')
  const date = new Date(`${canonical.replace(' ', 'T')}Z`)
  if (!/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(canonical) || !Number.isFinite(date.getTime()) || date.toISOString().slice(0, 19).replace('T', ' ') !== canonical || formatOracleExportDate(canonical, format) !== trimmed) throw new Error(`日期筛选值应使用 ${format} 格式，请检查日期是否有效。`)
  return trimmed
}

export function oracleExportFormattedTimePreview(source: ComputedReportValue, format: OracleExportDateFormat, now = new Date()) {
  if (source.kind === 'COLUMN_MAX') return ''
  const field = { type: 'str', control: 'DATETIME', format: 'YYYY-MM-DD HH:mm:ss' } as const
  if (source.kind === 'TIME_RANGE') {
    const beijing = new Date(now.getTime() + 8 * 3600000)
    const offset = source.code === 'PREVIOUS_MONTH' ? -1 : source.code === 'NEXT_MONTH' ? 1 : 0
    const month = new Date(Date.UTC(beijing.getUTCFullYear(), beijing.getUTCMonth() + offset, 1))
    const start = previewReportTimeDefault({ ...field, defaultTime: 'MONTH_START' }, month)
    const end = previewReportTimeDefault({ ...field, defaultTime: 'MONTH_END' }, month)
    return `当前预览：${formatOracleExportDate(start, format)} 至 ${formatOracleExportDate(end, format)}（含末日全天）；执行时按北京时间更新。`
  }
  let value = previewReportTimeDefault({ ...field, defaultTime: source.code }, now)
  if (/^\d{6}$/.test(value)) value = `${value}01`
  if (/^\d{4}$/.test(value)) value = `${value}0101`
  return `当前预览：${formatOracleExportDate(value, format)}${source.code === 'MONTH_END' || source.code === 'YEAR_END' ? '（日期上限含末日全天）' : ''}；执行时按北京时间更新。`
}
