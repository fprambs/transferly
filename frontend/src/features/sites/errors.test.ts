import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { siteErrorKeyOf } from './errors'

/**
 * Covers {@link siteErrorKeyOf}.
 *
 * Scenarios:
 * - known codes keep their own message
 * - unknown codes and errors that are not API errors get the generic message
 */
describe('siteErrorKeyOf', () => {
  it('keeps a known code', () => {
    const error = new ApiError({ type: 'about:blank', title: 'x', status: 409, code: 'collection_cycle' })

    expect(siteErrorKeyOf(error)).toBe('collection_cycle')
  })

  it('falls back to unknown for an unexpected code', () => {
    expect(siteErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 500, code: 'boom' }))).toBe('unknown')
  })

  it('falls back to unknown for a foreign error', () => {
    expect(siteErrorKeyOf(new Error('boom'))).toBe('unknown')
  })
})
