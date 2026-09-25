import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { explorerErrorKeyOf } from './errors'

/**
 * Covers {@link explorerErrorKeyOf}.
 *
 * Scenarios:
 * - known codes keep their own message
 * - unknown codes and errors that are not API errors get the generic message
 */
describe('explorerErrorKeyOf', () => {
  it('keeps a known code', () => {
    expect(explorerErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 404, code: 'path_not_found' }))).toBe('path_not_found')
  })

  it('falls back to unknown', () => {
    expect(explorerErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 500, code: 'boom' }))).toBe('unknown')
    expect(explorerErrorKeyOf(new Error('boom'))).toBe('unknown')
  })
})
