import { describe, expect, it } from 'vitest'

import { applyTheme, defaultThemeId, findTheme, resolveTheme, systemLightThemeId, themes } from './index'

/**
 * Covers {@link resolveTheme}.
 *
 * Scenarios:
 * - a fixed choice wears the chosen theme whatever the operating system prefers
 * - following the system wears the default dark theme by night and the Transferly light theme by day
 * - an unknown identifier falls back to the default theme
 */
describe('resolveTheme', () => {
  it.each([
    ['monokai', true],
    ['monokai', false],
    ['github-light', true],
  ])('wears the chosen theme %s when prefersDark is %s', (themeId, prefersDark) => {
    expect(resolveTheme({ themeId, followSystem: false }, prefersDark).id).toBe(themeId)
  })

  it('follows the operating system when asked to', () => {
    const choice = { themeId: 'monokai', followSystem: true }

    expect(resolveTheme(choice, true).id).toBe(defaultThemeId)
    expect(resolveTheme(choice, false).id).toBe(systemLightThemeId)
  })

  it('falls back to the default theme for an unknown identifier', () => {
    expect(resolveTheme({ themeId: 'no-such-theme', followSystem: false }, false).id).toBe(defaultThemeId)
  })
})

/**
 * Covers {@link findTheme}.
 *
 * Scenarios:
 * - finds a theme by identifier and reports an unknown one as missing
 */
describe('findTheme', () => {
  it('finds themes by identifier', () => {
    expect(findTheme('nord')?.name).toBe('Nord')
    expect(findTheme('missing')).toBeUndefined()
  })
})

/**
 * Covers {@link applyTheme}.
 *
 * Scenarios:
 * - a dark theme sets every custom property, the dark class, the dark color scheme and the theme attribute
 * - a light theme removes the dark class and sets the light color scheme
 * - applying another theme replaces the previous values
 */
describe('applyTheme', () => {
  it('applies a dark theme', () => {
    const root = document.createElement('html')
    const monokai = findTheme('monokai')!

    applyTheme(root, monokai)

    expect(root.style.getPropertyValue('--background')).toBe(monokai.tokens.bg)
    expect(root.style.getPropertyValue('--primary')).toBe(monokai.tokens.accent)
    expect(root.style.getPropertyValue('--surface')).toBe(monokai.tokens.surface)
    expect(root).toHaveClass('dark')
    expect(root.style.colorScheme).toBe('dark')
    expect(root.dataset.theme).toBe('monokai')
  })

  it('applies a light theme', () => {
    const root = document.createElement('html')
    root.classList.add('dark')

    applyTheme(root, findTheme('github-light')!)

    expect(root).not.toHaveClass('dark')
    expect(root.style.colorScheme).toBe('light')
  })

  it('replaces the values of the previous theme', () => {
    const root = document.createElement('html')

    applyTheme(root, findTheme('dracula')!)
    applyTheme(root, findTheme('nord')!)

    expect(root.style.getPropertyValue('--background')).toBe(findTheme('nord')!.tokens.bg)
    expect(root.dataset.theme).toBe('nord')
    expect(themes.length).toBeGreaterThan(1)
  })
})
