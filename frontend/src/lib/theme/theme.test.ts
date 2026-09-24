import { describe, expect, it } from 'vitest'

import { applyTheme, resolveTheme, themePreferences } from './index'

/**
 * Covers {@link resolveTheme}.
 *
 * Scenarios:
 * - explicit light and dark choices ignore the operating system
 * - the system choice follows the operating system preference
 */
describe('resolveTheme', () => {
  it.each([
    ['light', true, 'light'],
    ['light', false, 'light'],
    ['dark', false, 'dark'],
    ['dark', true, 'dark'],
    ['system', true, 'dark'],
    ['system', false, 'light'],
  ] as const)('resolves %s with prefersDark=%s to %s', (preference, prefersDark, expected) => {
    expect(resolveTheme(preference, prefersDark)).toBe(expected)
  })

  it('lists the selectable preferences in display order', () => {
    expect(themePreferences).toEqual(['light', 'dark', 'system'])
  })
})

/**
 * Covers {@link applyTheme}.
 *
 * Scenarios:
 * - dark adds the dark class and sets the CSS color scheme
 * - light removes the dark class again
 */
describe('applyTheme', () => {
  it('toggles the dark class and the color scheme', () => {
    const root = document.createElement('html')

    applyTheme(root, 'dark')
    expect(root.classList.contains('dark')).toBe(true)
    expect(root.style.colorScheme).toBe('dark')

    applyTheme(root, 'light')
    expect(root.classList.contains('dark')).toBe(false)
    expect(root.style.colorScheme).toBe('light')
  })
})
