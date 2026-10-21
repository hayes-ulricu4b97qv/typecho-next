const formatterCache = new Map<string, Intl.DateTimeFormat>()

export interface SiteDateParts {
  year: string
  month: string
  day: string
}

function siteDateFormatter(timeZone: string) {
  const requestedZone = timeZone.trim() || 'UTC'
  const cached = formatterCache.get(requestedZone)
  if (cached) return cached

  const options: Intl.DateTimeFormatOptions = {
    calendar: 'gregory',
    day: '2-digit',
    month: '2-digit',
    numberingSystem: 'latn',
    timeZone: requestedZone,
    year: 'numeric',
  }
  let formatter: Intl.DateTimeFormat
  try {
    formatter = new Intl.DateTimeFormat('zh-CN', options)
  } catch {
    formatter = new Intl.DateTimeFormat('zh-CN', { ...options, timeZone: 'UTC' })
  }
  formatterCache.set(requestedZone, formatter)
  return formatter
}

export function siteDateParts(value: string, timeZone: string): SiteDateParts | null {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const parts = siteDateFormatter(timeZone).formatToParts(date)
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((item) => item.type === type)?.value ?? ''
  const result = { year: part('year'), month: part('month'), day: part('day') }
  return result.year && result.month && result.day ? result : null
}

export function formatSiteDate(value: string, timeZone: string) {
  const parts = siteDateParts(value, timeZone)
  if (!parts) return '—'
  return `${parts.year}年${Number(parts.month)}月${Number(parts.day)}日`
}
