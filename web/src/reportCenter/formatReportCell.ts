import { formatShanghaiDateTime } from '../dateTime.js'

export function formatReportCell(value: unknown, nullDisplay = '—', valueType = ''): string {
  if (value === null || value === undefined) return nullDisplay || '—'
  if ((valueType === 'datetime' || valueType === 'timestamp') && (typeof value === 'string' || typeof value === 'number' || value instanceof Date)) {
    return formatShanghaiDateTime(value, String(value))
  }
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}
