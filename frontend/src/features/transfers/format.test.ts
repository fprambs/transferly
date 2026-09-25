import { describe, expect, it } from 'vitest'

import { formatDuration, formatSpeed, percentDone } from './format'

/**
 * Covers the transfer formatting helpers.
 *
 * Scenarios:
 * - speeds use the size units with a per-second suffix
 * - durations use seconds, minutes with padded seconds, and hours with padded minutes
 * - the percentage rounds, caps at 100 and is unknown without a positive total
 */
describe('transfer formatting', () => {
  it('formats speeds', () => {
    expect(formatSpeed(0)).toBe('0 B/s')
    expect(formatSpeed(1536)).toBe('1.5 KB/s')
  })

  it.each([
    [0, '0s'],
    [59, '59s'],
    [60, '1m 00s'],
    [185, '3m 05s'],
    [3600, '1h 00m'],
    [3725, '1h 02m'],
  ])('formats %d seconds as %s', (seconds, expected) => {
    expect(formatDuration(seconds)).toBe(expected)
  })

  it('computes the percentage', () => {
    expect(percentDone(1, 3)).toBe(33)
    expect(percentDone(5, 3)).toBe(100)
    expect(percentDone(1, undefined)).toBeUndefined()
    expect(percentDone(1, 0)).toBeUndefined()
  })
})
