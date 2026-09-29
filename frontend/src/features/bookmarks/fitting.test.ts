import { describe, expect, it } from 'vitest'

import { fitting } from './fitting'

/**
 * Covers {@link fitting} (4.14g).
 *
 * Scenarios:
 * - chips that fit with their gaps are all shown, none included
 * - otherwise as many are shown as leave room for the button that holds the rest, which may be none
 */
describe('fitting', () => {
  it('shows every chip that fits', () => {
    expect(fitting([100, 100], 204)).toBe(2)
    expect(fitting([], 0)).toBe(0)
  })

  it('leaves room for the button that holds the rest', () => {
    expect(fitting([100, 100, 100], 250)).toBe(2)
    expect(fitting([100, 100, 100], 200)).toBe(1)
    expect(fitting([300], 200)).toBe(0)
  })
})
