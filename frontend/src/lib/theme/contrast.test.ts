import { describe, expect, it } from 'vitest'

import { contrastRatio, luminance } from './contrast'

/**
 * Covers {@link luminance}.
 *
 * Scenarios:
 * - black is 0 and white is 1
 * - low and high channel values follow the two branches of the sRGB curve
 */
describe('luminance', () => {
  it('is 0 for black and 1 for white', () => {
    expect(luminance('#000000')).toBe(0)
    expect(luminance('#ffffff')).toBeCloseTo(1, 10)
  })

  it('applies the sRGB curve to dark and light channels', () => {
    expect(luminance('#0a0a0a')).toBeCloseTo(0.003, 3)
    expect(luminance('#808080')).toBeCloseTo(0.2159, 3)
  })
})

/**
 * Covers {@link contrastRatio}.
 *
 * Scenarios:
 * - black on white is 21 and a color against itself is 1
 * - the ratio does not depend on the order of the colors
 */
describe('contrastRatio', () => {
  it('matches the WCAG reference values', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5)
    expect(contrastRatio('#336699', '#336699')).toBe(1)
  })

  it('is symmetric', () => {
    expect(contrastRatio('#123456', '#fedcba')).toBe(contrastRatio('#fedcba', '#123456'))
  })
})
