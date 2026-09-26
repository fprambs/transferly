import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { forgetErrorOf } from './errors'

/**
 * Covers {@link forgetErrorOf}.
 *
 * Scenarios:
 * - known codes keep their own message
 * - unknown codes and errors that are not API errors get the generic message
 */
describe('forgetErrorOf', () => {
  it('keeps a known code', () => {
    expect(forgetErrorOf(new ApiError({ type: 'about:blank', title: 'x', status: 403, code: 'forbidden' }))).toBe('forbidden')
  })

  it('falls back to unknown', () => {
    expect(forgetErrorOf(new ApiError({ type: 'about:blank', title: 'x', status: 500, code: 'boom' }))).toBe('unknown')
    expect(forgetErrorOf(new Error('boom'))).toBe('unknown')
  })
})
