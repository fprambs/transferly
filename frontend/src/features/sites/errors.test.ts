import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { configFailureOf, fieldOf, siteErrorKeyOf } from './errors'

/** A refusal of the server with a code and parameters. */
function refusal(code: string, params?: Record<string, unknown>): ApiError {
  return new ApiError({ type: 'about:blank', title: 'x', status: 422, code, params })
}

/**
 * Covers {@link siteErrorKeyOf}, {@link fieldOf} and {@link configFailureOf}.
 *
 * Scenarios:
 * - known codes keep their own message, and a refused key of the jump host is told apart by its field
 * - unknown codes and errors that are not API errors get the generic message
 * - the field a refusal names is found, and a refused setting is read with its reason; other failures name none
 */
describe('site errors', () => {
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

  it('tells a refused key of the jump host apart', () => {
    expect(siteErrorKeyOf(refusal('wrong_passphrase', { field: 'jump_private_key' }))).toBe('jump_wrong_passphrase')
    expect(siteErrorKeyOf(refusal('wrong_passphrase', { field: 'private_key' }))).toBe('wrong_passphrase')
  })

  it('finds the field a refusal names and the setting the server refused', () => {
    expect(fieldOf(refusal('wrong_passphrase', { field: 'private_key' }))).toBe('private_key')
    expect(fieldOf(new Error('boom'))).toBeUndefined()
    expect(configFailureOf(refusal('invalid_config', { field: 'port', reason: 'range' }))).toEqual({ field: 'port', reason: 'range' })
    expect(configFailureOf(refusal('invalid_config'))).toBeUndefined()
    expect(configFailureOf(refusal('wrong_passphrase', { field: 'private_key' }))).toBeUndefined()
    expect(configFailureOf(new Error('boom'))).toBeUndefined()
  })
})
