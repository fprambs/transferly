import { describe, expect, it } from 'vitest'

import { matcherFor } from './match'

/**
 * Covers {@link matcherFor}.
 *
 * Scenarios:
 * - plain text matches anywhere in the name, case-insensitively
 * - a pattern with `*` or `?` matches the whole name as a glob, case-insensitively
 * - special regular expression characters in plain text are taken literally
 * - a blank query, or one that is only spaces, matches nothing
 */
describe('matcherFor', () => {
  it('matches plain text anywhere in the name, case-insensitively', () => {
    const matches = matcherFor('report')
    expect(matches('quarterly-report.csv')).toBe(true)
    expect(matches('QUARTERLY-REPORT.CSV')).toBe(true)
    expect(matches('invoice.pdf')).toBe(false)
  })

  it('matches a glob against the whole name, case-insensitively', () => {
    const matches = matcherFor('*.CSV')
    expect(matches('report.csv')).toBe(true)
    expect(matches('report.csv.bak')).toBe(false)
    expect(matcherFor('backup-????.zip')('backup-2026.zip')).toBe(true)
    expect(matcherFor('backup-????.zip')('backup-26.zip')).toBe(false)
  })

  it('takes special regular expression characters of plain text literally', () => {
    expect(matcherFor('a.b')('a.b')).toBe(true)
    expect(matcherFor('a.b')('aXb')).toBe(false)
    expect(matcherFor('(draft)')('final (draft) v2')).toBe(true)
  })

  it('matches nothing for a blank query', () => {
    expect(matcherFor('')('anything')).toBe(false)
    expect(matcherFor('   ')('anything')).toBe(false)
  })
})
