import { AxeBuilder } from '@axe-core/playwright'

import type { Language, Theme } from '../pages/shell-page.ts'
import { expect, test } from '../fixtures.ts'

const cases: ReadonlyArray<{ language: Language; theme: Theme }> = [
  { language: 'en', theme: 'light' },
  { language: 'en', theme: 'dark' },
  { language: 'es', theme: 'light' },
  { language: 'es', theme: 'dark' },
]

/**
 * Automated accessibility scan of the application shell against WCAG 2.1 A and AA.
 *
 * Scenarios:
 * - no violations in English and Spanish, in the light and dark theme
 */
test.describe('accessibility', () => {
  for (const { language, theme } of cases) {
    test(`has no WCAG violations in ${language} with the ${theme} theme`, async ({ shell, page }) => {
      await shell.setLanguage(language)
      await shell.setTheme(theme)

      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()

      expect(results.violations).toEqual([])
    })
  }
})
