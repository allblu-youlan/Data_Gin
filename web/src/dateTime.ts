export const SHANGHAI_TIME_ZONE = 'Asia/Shanghai'

type DateTimeValue = string | number | Date | null | undefined

const formatter = new Intl.DateTimeFormat('en-CA', {
  timeZone: SHANGHAI_TIME_ZONE,
  year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
})

// API timestamps without an offset represent Shanghai wall-clock time.
export function parseShanghaiDateTime(value: DateTimeValue): Date | null {
  if (value === null || value === undefined || value === '') return null
  let date: Date
  if (typeof value === 'string') {
    let normalized = value.trim()
    if (!/^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?$/i.test(normalized)) return null
    normalized = normalized.replace(' ', 'T')
    if (normalized.length === 10) normalized += 'T00:00:00'
    if (!/(?:Z|[+-]\d{2}:?\d{2})$/i.test(normalized)) normalized += '+08:00'
    date = new Date(normalized)
  } else {
    date = new Date(value instanceof Date ? value.getTime() : value)
  }
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatShanghaiDateTime(value: DateTimeValue, fallback = '—'): string {
  const date = parseShanghaiDateTime(value)
  if (!date) return fallback
  const parts = formatter.formatToParts(date)
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find(item => item.type === type)?.value ?? ''
  return `${part('year')}-${part('month')}-${part('day')} ${part('hour')}:${part('minute')}:${part('second')}`
}

export function formatShanghaiDate(value: DateTimeValue, fallback = '—'): string {
  const formatted = formatShanghaiDateTime(value, '')
  return formatted ? formatted.slice(0, 10) : fallback
}

export function toShanghaiDateTimeInput(value: DateTimeValue): string {
  return formatShanghaiDateTime(value, '').replace(' ', 'T')
}
