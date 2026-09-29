import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { changedHostKeyOf, quickErrorKeyOf, unknownHostKeyOf } from './errors'

/** Builds an API error with a code and optional params. */
function failure(code: string, params?: Record<string, unknown>): ApiError {
  return new ApiError({ type: 'about:blank', title: code, status: 409, code, params })
}

/**
 * Covers {@link quickErrorKeyOf}, {@link unknownHostKeyOf} and {@link changedHostKeyOf}.
 *
 * Scenarios:
 * - known codes keep their own message, unknown codes and foreign errors get the generic one
 * - an unknown host key failure yields its key type and fingerprint
 * - other failures, incomplete params and foreign errors yield nothing
 * - a changed host key failure yields its key and the trusted fingerprints, dropping what is not text, or none when they are missing
 */
describe('quick connect errors', () => {
  it('keeps a known code', () => {
    expect(quickErrorKeyOf(failure('remote_auth_failed'))).toBe('remote_auth_failed')
  })

  it('falls back to unknown for an unexpected code', () => {
    expect(quickErrorKeyOf(failure('boom'))).toBe('unknown')
  })

  it('falls back to unknown for an error that is not an API error', () => {
    expect(quickErrorKeyOf(new Error('boom'))).toBe('unknown')
  })

  it('reads the key of an unknown host key failure', () => {
    expect(unknownHostKeyOf(failure('host_key_unknown', { keyType: 'ssh-ed25519', fingerprint: 'SHA256:x' }))).toEqual({
      keyType: 'ssh-ed25519',
      fingerprint: 'SHA256:x',
    })
  })

  it.each([
    ['another code', failure('boom')],
    ['missing params', failure('host_key_unknown')],
    ['a missing fingerprint', failure('host_key_unknown', { keyType: 'ssh-ed25519' })],
    ['a foreign error', new Error('boom')],
  ])('yields nothing for %s', (_name, error) => {
    expect(unknownHostKeyOf(error)).toBeUndefined()
  })

  it('reads the key of a changed host key failure and the keys trusted before it', () => {
    expect(changedHostKeyOf(failure('host_key_changed', { keyType: 'ssh-ed25519', fingerprint: 'SHA256:new', trusted: ['SHA256:old', 7] }))).toEqual({
      keyType: 'ssh-ed25519',
      fingerprint: 'SHA256:new',
      trusted: ['SHA256:old'],
    })
    expect(changedHostKeyOf(failure('host_key_changed', { keyType: 'ssh-ed25519', fingerprint: 'SHA256:new' }))?.trusted).toEqual([])
  })

  it.each([
    ['another code', failure('host_key_unknown', { keyType: 'ssh-ed25519', fingerprint: 'SHA256:x' })],
    ['missing params', failure('host_key_changed')],
    ['a foreign error', new Error('boom')],
  ])('yields no changed key for %s', (_name, error) => {
    expect(changedHostKeyOf(error)).toBeUndefined()
  })
})
