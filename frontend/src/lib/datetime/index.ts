const rfc3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/

const fixedOffset = /^[+-]\d{2}(:?\d{2})?$/

const relativeUnits = [
  { unit: 'year', seconds: 365 * 24 * 3600 },
  { unit: 'month', seconds: 30 * 24 * 3600 },
  { unit: 'day', seconds: 24 * 3600 },
  { unit: 'hour', seconds: 3600 },
  { unit: 'minute', seconds: 60 },
  { unit: 'second', seconds: 1 },
] as const

const formatterCache = new Map<string, Intl.DateTimeFormat>()

/** Options for {@link formatInstant}. */
export interface FormatOptions {
  /** BCP 47 locale used for the layout and language of the result. */
  locale: string
  /** IANA time zone name the instant is displayed in. */
  timeZone: string
  /** Date part style; omitted parts are not shown. */
  dateStyle?: Intl.DateTimeFormatOptions['dateStyle']
  /** Time part style; omitted parts are not shown. */
  timeStyle?: Intl.DateTimeFormatOptions['timeStyle']
}

/**
 * Parses an RFC 3339 timestamp that carries an explicit `Z` or numeric offset.
 *
 * @param value - Timestamp such as `2026-03-08T07:00:00Z`.
 * @returns The instant it denotes.
 * @throws {RangeError} When the value has no explicit offset or is not a real date.
 */
export function parseInstant(value: string): Date {
  if (!rfc3339.test(value)) {
    throw new RangeError(`Not an RFC 3339 timestamp with an explicit offset: ${value}`)
  }
  const instant = new Date(value)
  if (Number.isNaN(instant.getTime())) {
    throw new RangeError(`Not a valid date: ${value}`)
  }
  return instant
}

/**
 * Reports whether a value is an IANA time zone name such as `America/Santiago`.
 * Fixed offsets such as `+01:00` are rejected because they ignore daylight saving time.
 */
export function isValidTimeZone(timeZone: string): boolean {
  if (timeZone === '' || fixedOffset.test(timeZone)) {
    return false
  }
  try {
    new Intl.DateTimeFormat('en', { timeZone })
    return true
  } catch {
    return false
  }
}

/** Returns the IANA time zone name the browser is configured with. */
export function resolveTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

/**
 * Formats an instant for display in a given locale and time zone.
 *
 * @param value - The instant, or an RFC 3339 timestamp with an explicit offset.
 * @param options - Locale, IANA time zone and date and time styles.
 * @returns The localized text. Without styles, medium date and short time are used.
 * @throws {RangeError} When the timestamp or the time zone is invalid.
 */
export function formatInstant(value: Date | string, options: FormatOptions): string {
  const instant = typeof value === 'string' ? parseInstant(value) : value
  const { locale, timeZone } = options
  const hasStyle = options.dateStyle !== undefined || options.timeStyle !== undefined
  const dateStyle = hasStyle ? options.dateStyle : 'medium'
  const timeStyle = hasStyle ? options.timeStyle : 'short'
  const key = `${locale}|${timeZone}|${dateStyle ?? ''}|${timeStyle ?? ''}`

  let formatter = formatterCache.get(key)
  if (formatter === undefined) {
    formatter = new Intl.DateTimeFormat(locale, { timeZone, dateStyle, timeStyle })
    formatterCache.set(key, formatter)
  }
  return formatter.format(instant)
}

/**
 * Formats the distance between two instants as approximate relative text,
 * such as "2 days ago" or "in 3 hours".
 *
 * @param value - The instant being described.
 * @param now - The reference instant.
 * @param locale - BCP 47 locale of the result.
 */
export function formatRelative(value: Date, now: Date, locale: string): string {
  const seconds = (value.getTime() - now.getTime()) / 1000
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const match =
    relativeUnits.find(({ seconds: size }) => Math.abs(seconds) >= size) ??
    relativeUnits[relativeUnits.length - 1]
  return formatter.format(Math.trunc(seconds / match.seconds), match.unit)
}
