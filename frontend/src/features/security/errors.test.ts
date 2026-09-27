import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { needsSecondStep, securityErrorKeyOf } from './errors'

/**
 * Covers {@link securityErrorKeyOf} and {@link needsSecondStep}.
 *
 * Scenarios:
 * - known codes keep their own message, and unknown codes and foreign errors get the generic one
 * - an answer with a challenge asks for the second step and one with a session does not
 */
describe('security errors', () => {
  it('maps codes to messages', () => {
    expect(securityErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 401, code: 'invalid_code' }))).toBe('invalid_code')
    expect(securityErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 500, code: 'boom' }))).toBe('unknown')
    expect(securityErrorKeyOf(new Error('boom'))).toBe('unknown')
  })

  it('tells a challenge from a session', () => {
    expect(needsSecondStep({ challenge: 'c', methods: ['totp'] })).toBe(true)
    expect(needsSecondStep({ user: {}, csrfToken: 't' })).toBe(false)
  })
})
