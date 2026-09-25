import { describe, expect, it } from 'vitest'

import { uuid } from './uuid'

/**
 * Covers {@link uuid}.
 *
 * Scenarios:
 * - the result has the shape of a version 4 UUID with the variant bits set
 * - two calls give different values
 */
describe('uuid', () => {
  it('has the shape of a version 4 UUID', () => {
    for (let index = 0; index < 50; index++) {
      expect(uuid()).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    }
  })

  it('gives different values', () => {
    expect(uuid()).not.toBe(uuid())
  })
})
