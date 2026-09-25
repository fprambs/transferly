import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { errorKeyOf } from './errors'

/** Builds an API error with a code. */
function failure(code: string): ApiError {
  return new ApiError({ type: 'about:blank', title: code, status: 400, code })
}

/**
 * Covers {@link errorKeyOf}.
 *
 * Scenarios:
 * - known server codes map to their own message
 * - unknown codes and foreign errors map to the generic message
 */
describe('errorKeyOf', () => {
  it.each(['invalid_credentials', 'rate_limited', 'setup_complete', 'network_error'])('keeps %s', (code) => {
    expect(errorKeyOf(failure(code))).toBe(code)
  })

  it('falls back to unknown for an unexpected code', () => {
    expect(errorKeyOf(failure('boom'))).toBe('unknown')
  })

  it('falls back to unknown for an error that is not an API error', () => {
    expect(errorKeyOf(new Error('boom'))).toBe('unknown')
  })
})
