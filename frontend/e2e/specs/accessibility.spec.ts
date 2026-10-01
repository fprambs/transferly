import { AxeBuilder } from '@axe-core/playwright'

import { expect, test } from '../fixtures.ts'
import { settle } from '../settle.ts'

const themes = [
  'transferly-dark',
  'transferly-light',
  'monokai',
  'dracula',
  'nord',
  'one-dark',
  'tokyo-night',
  'gruvbox-dark',
  'solarized-dark',
  'github-dark',
  'solarized-light',
  'github-light',
]

/**
 * Automated accessibility scan of the application shell against WCAG 2.1 A and AA.
 *
 * Scenarios:
 * - no violations in any of the twelve themes, which checks their contrast in the real page
 * - no violations in Spanish with the default theme
 */
test.describe('accessibility', () => {
  for (const theme of themes) {
    test(`has no WCAG violations with the ${theme} theme`, async ({ shell, page }) => {
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await shell.setTheme(theme)

      await settle(page)
      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()

      expect(results.violations).toEqual([])
    })
  }

  test('has no WCAG violations in Spanish', async ({ shell, page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await shell.setLanguage('es')

    await settle(page)
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()

    expect(results.violations).toEqual([])
  })
})
