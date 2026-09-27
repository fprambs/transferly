import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'

import { syncErrorKeyOf } from './errors'

/**
 * Covers {@link syncErrorKeyOf}.
 *
 * Scenarios:
 * - a known code of the file endpoints maps to its key, and an unknown one to the generic key
 * - anything that is not an API failure, such as a local file system error, maps to the local key
 */
describe('syncErrorKeyOf', () => {
  it('maps known and unknown codes', () => {
    expect(syncErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 404, code: 'path_not_found' }))).toBe('path_not_found')
    expect(syncErrorKeyOf(new ApiError({ type: 'about:blank', title: 'x', status: 500, code: 'something_else' }))).toBe('unknown')
  })

  it('maps anything else to the local failure key', () => {
    expect(syncErrorKeyOf(new DOMException('nope', 'NotFoundError'))).toBe('localFailure')
    expect(syncErrorKeyOf(new Error('boom'))).toBe('localFailure')
  })
})
