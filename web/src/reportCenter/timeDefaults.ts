import type { ReportInputField, ReportTimeDefault } from './types'

const timeDefaults: Array<{ value: ReportTimeDefault; label: string }> = [
  { value: 'CURRENT_MONTH', label: '当月（YYYYMM）' },
  { value: 'PREVIOUS_MONTH', label: '上月（YYYYMM）' },
  { value: 'NEXT_MONTH', label: '下月（YYYYMM）' },
  { value: 'CURRENT_YEAR', label: '当年（YYYY）' },
  { value: 'TODAY', label: '今天' },
  { value: 'YESTERDAY', label: '昨天' },
  { value: 'MONTH_START', label: '本月第一天' },
  { value: 'MONTH_END', label: '本月最后一天' },
  { value: 'YEAR_START', label: '本年第一天' },
  { value: 'YEAR_END', label: '本年最后一天' },
  { value: 'NOW', label: '当前时间（精确到秒）' },
]

export function reportTimeDefaultOptions(field: Pick<ReportInputField, 'type' | 'control'>) {
  if (!['str', 'number'].includes(field.type) || field.control === 'SELECT' || field.control === 'CHECKBOX') return []
  if (field.control === 'DATE' || field.control === 'DATETIME') {
    return timeDefaults.filter(({ value }) => !['CURRENT_MONTH', 'PREVIOUS_MONTH', 'NEXT_MONTH', 'CURRENT_YEAR'].includes(value) && (field.control !== 'DATE' || value !== 'NOW'))
  }
  return timeDefaults
}

export function isReportTimeDefault(value: unknown): value is ReportTimeDefault {
  return timeDefaults.some((option) => option.value === value)
}

export function reportTimeDefaultLabel(value: ReportTimeDefault) {
  return timeDefaults.find((option) => option.value === value)?.label ?? value
}

// Preview only: run requests leave automatic values unset so the server uses
// the execution time in Asia/Shanghai, even if the page stays open overnight.
export function previewReportTimeDefault(field: Pick<ReportInputField, 'defaultTime' | 'type' | 'control' | 'format'>, now = new Date()): string {
  if (!field.defaultTime) return ''
  const beijing = new Date(now.getTime() + 8 * 60 * 60 * 1000)
  const year = beijing.getUTCFullYear(), month = beijing.getUTCMonth(), day = beijing.getUTCDate()
  let date = new Date(Date.UTC(year, month, day))
  switch (field.defaultTime) {
    case 'CURRENT_MONTH': return `${year}${pad(month + 1)}`
    case 'PREVIOUS_MONTH': date = new Date(Date.UTC(year, month - 1, 1)); return `${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}`
    case 'NEXT_MONTH': date = new Date(Date.UTC(year, month + 1, 1)); return `${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}`
    case 'CURRENT_YEAR': return String(year)
    case 'YESTERDAY': date.setUTCDate(day - 1); break
    case 'MONTH_START': date.setUTCDate(1); break
    case 'MONTH_END': date = new Date(Date.UTC(year, month + 1, 0)); break
    case 'YEAR_START': date = new Date(Date.UTC(year, 0, 1)); break
    case 'YEAR_END': date = new Date(Date.UTC(year, 11, 31)); break
    case 'NOW': date = beijing; break
  }
  const y = date.getUTCFullYear(), m = pad(date.getUTCMonth() + 1), d = pad(date.getUTCDate())
  const h = pad(date.getUTCHours()), min = pad(date.getUTCMinutes()), sec = pad(date.getUTCSeconds())
  if (field.control === 'DATE') return field.format === 'YYYY-MM-DD' ? `${y}-${m}-${d}` : `${y}${m}${d}`
  if (field.control === 'DATETIME') {
    if (field.format === 'YYYYMMDDHHmmss') return `${y}${m}${d}${h}${min}${sec}`
    return `${y}-${m}-${d}${field.format === 'YYYY-MM-DD HH:mm:ss' ? ' ' : 'T'}${h}:${min}:${sec}`
  }
  return `${y}${m}${d}${field.defaultTime === 'NOW' ? `${h}${min}${sec}` : ''}`
}

function pad(value: number) { return String(value).padStart(2, '0') }
