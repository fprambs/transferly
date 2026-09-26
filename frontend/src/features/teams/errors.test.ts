import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { teamErrorKeyOf } from './errors'

/**
 * Covers {@link teamErrorKeyOf}.
 *
 * Scenarios:
 * - known codes keep their own message
 * - unknown codes and errors that are not API errors get the generic message
 */
describe('teamErrorKeyOf', () => {
  it('keeps a known code', () => {
    expect(teamErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 409, code: 'owner_bound' }))).toBe('owner_bound')
  })

  it('falls back to unknown for an unexpected code or a foreign error', () => {
    expect(teamErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 500, code: 'boom' }))).toBe('unknown')
    expect(teamErrorKeyOf(new Error('boom'))).toBe('unknown')
  })
})
