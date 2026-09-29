import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { bookmarkErrorKeyOf } from './errors'

/**
 * Covers {@link bookmarkErrorKeyOf}.
 *
 * Scenarios:
 * - a known code keeps its own message, an unknown code and an error that is not from the server get the generic one
 */
describe('bookmark errors', () => {
  it('maps codes to their messages', () => {
    const failure = (code: string) => new ApiError({ type: 'about:blank', title: code, status: 409, code })
    expect(bookmarkErrorKeyOf(failure('bookmark_limit_reached'))).toBe('bookmark_limit_reached')
    expect(bookmarkErrorKeyOf(failure('boom'))).toBe('unknown')
    expect(bookmarkErrorKeyOf(new Error('boom'))).toBe('unknown')
  })
})
