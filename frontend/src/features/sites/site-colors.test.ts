import { describe, expect, it } from 'vitest'

import { colorEdge, colorFill, siteColors } from './site-colors'

/**
 * Covers {@link colorFill} and {@link colorEdge}.
 *
 * Scenarios:
 * - every label color has a fill and a tab edge drawn from its own `--label-*` tone
 * - a site without a color has neither
 */
describe('site colors', () => {
  it('draws every color from its own tone, and nothing without one', () => {
    for (const color of siteColors) {
      expect(colorFill(color)).toBe(`bg-(--label-${color})`)
      expect(colorEdge(color)).toBe(`border-t-(--label-${color})`)
    }
    expect(colorFill(undefined)).toBeUndefined()
    expect(colorEdge(undefined)).toBeUndefined()
  })
})
