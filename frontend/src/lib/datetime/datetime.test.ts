import { describe, expect, it } from 'vitest'

import {
  formatInstant,
  formatRelative,
  isValidTimeZone,
  parseInstant,
  resolveTimeZone,
} from './index'

/**
 * Covers {@link parseInstant}.
 *
 * Scenarios:
 * - accepts `Z`, numeric offsets and fractional seconds
 * - rejects timestamps without an offset, date-only values and free text
 * - rejects well-formed values that are not real dates
 */
describe('parseInstant', () => {
  it.each([
    ['2026-03-08T07:00:00Z', '2026-03-08T07:00:00.000Z'],
    ['2026-03-08T04:00:00-03:00', '2026-03-08T07:00:00.000Z'],
    ['2026-03-08T07:00:00.250Z', '2026-03-08T07:00:00.250Z'],
  ])('accepts %s', (input, expected) => {
    expect(parseInstant(input).toISOString()).toBe(expected)
  })

  it.each(['2026-03-08T07:00:00', '2026-03-08', 'yesterday', ''])(
    'rejects %j because the offset is ambiguous or missing',
    (input) => {
      expect(() => parseInstant(input)).toThrow(RangeError)
    },
  )

  it('rejects well-formed values that are not real dates', () => {
    expect(() => parseInstant('2026-13-40T99:00:00Z')).toThrow(RangeError)
  })
})

/**
 * Covers {@link isValidTimeZone} and {@link resolveTimeZone}.
 *
 * Scenarios:
 * - accepts IANA names, including half-hour and quarter-hour zones
 * - rejects unknown names, empty values and fixed offsets
 * - the browser zone is always a valid IANA name
 */
describe('time zone names', () => {
  it.each(['UTC', 'America/Santiago', 'Asia/Kolkata', 'Asia/Kathmandu'])('accepts %s', (zone) => {
    expect(isValidTimeZone(zone)).toBe(true)
  })

  it.each(['Mars/Olympus', '', '+01:00', '-0300', '+05'])('rejects %j', (zone) => {
    expect(isValidTimeZone(zone)).toBe(false)
  })

  it('resolves the browser zone to a valid name', () => {
    expect(isValidTimeZone(resolveTimeZone())).toBe(true)
  })
})

/**
 * Covers {@link formatInstant}.
 *
 * Scenarios:
 * - the same instant renders differently per zone, including summer and winter offsets
 * - half-hour and quarter-hour offsets
 * - the daylight saving spring-forward gap is skipped
 * - the locale controls language and layout
 * - Date objects and RFC 3339 strings are equivalent
 * - invalid zones and ambiguous timestamps are rejected
 */
describe('formatInstant', () => {
  const time = { locale: 'en-GB', dateStyle: undefined, timeStyle: 'short' } as const

  it('renders the same instant in each zone', () => {
    const instant = '2026-01-15T15:00:00Z'
    expect(formatInstant(instant, { ...time, timeZone: 'UTC' })).toBe('15:00')
    expect(formatInstant(instant, { ...time, timeZone: 'America/Santiago' })).toBe('12:00')
    expect(formatInstant(instant, { ...time, timeZone: 'Asia/Tokyo' })).toBe('00:00')
  })

  it('applies the winter offset in the southern hemisphere', () => {
    const instant = '2026-07-15T15:00:00Z'
    expect(formatInstant(instant, { ...time, timeZone: 'America/Santiago' })).toBe('11:00')
  })

  it('supports half-hour and quarter-hour offsets', () => {
    const instant = '2026-01-15T15:00:00Z'
    expect(formatInstant(instant, { ...time, timeZone: 'Asia/Kolkata' })).toBe('20:30')
    expect(formatInstant(instant, { ...time, timeZone: 'Asia/Kathmandu' })).toBe('20:45')
  })

  it('skips the local hour that does not exist when clocks spring forward', () => {
    const zone = { ...time, timeZone: 'America/New_York' }
    expect(formatInstant('2026-03-08T06:59:00Z', zone)).toBe('01:59')
    expect(formatInstant('2026-03-08T07:00:00Z', zone)).toBe('03:00')
  })

  it('formats dates in the requested locale', () => {
    const date = { timeZone: 'UTC', dateStyle: 'medium' } as const
    expect(formatInstant('2026-01-15T15:00:00Z', { ...date, locale: 'en-GB' })).toBe('15 Jan 2026')
    expect(formatInstant('2026-01-15T15:00:00Z', { ...date, locale: 'es-ES' })).toBe('15 ene 2026')
  })

  it('accepts Date objects and strings interchangeably', () => {
    const options = { ...time, timeZone: 'UTC' }
    expect(formatInstant(new Date('2026-01-15T15:00:00Z'), options)).toBe(
      formatInstant('2026-01-15T15:00:00Z', options),
    )
  })

  it('uses a medium date and short time when no style is given', () => {
    const text = formatInstant('2026-01-15T15:00:00Z', { locale: 'en-GB', timeZone: 'UTC' })
    expect(text).toContain('15 Jan 2026')
    expect(text).toContain('15:00')
  })

  it('rejects unknown zones and timestamps without an offset', () => {
    expect(() => formatInstant('2026-01-15T15:00:00Z', { locale: 'en', timeZone: 'Mars/Olympus' })).toThrow(
      RangeError,
    )
    expect(() => formatInstant('2026-01-15T15:00:00', { locale: 'en', timeZone: 'UTC' })).toThrow(RangeError)
  })
})

/**
 * Covers {@link formatRelative}.
 *
 * Scenarios:
 * - past and future distances pick the largest fitting unit
 * - no distance reads as "now"
 * - the locale controls the wording
 */
describe('formatRelative', () => {
  const now = new Date('2026-06-15T12:00:00Z')

  it.each([
    ['2026-06-15T11:58:30Z', '1 minute ago'],
    ['2026-06-13T12:00:00Z', '2 days ago'],
    ['2026-06-15T15:00:00Z', 'in 3 hours'],
    ['2026-06-15T12:00:00Z', 'now'],
  ])('describes %s in English as %s', (value, expected) => {
    expect(formatRelative(new Date(value), now, 'en')).toBe(expected)
  })

  it('describes the distance in Spanish', () => {
    expect(formatRelative(new Date('2026-06-12T12:00:00Z'), now, 'es')).toBe('hace 3 días')
    expect(formatRelative(new Date('2026-06-15T15:00:00Z'), now, 'es')).toBe('dentro de 3 horas')
  })
})
