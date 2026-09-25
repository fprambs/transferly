import type { Language, Theme } from '../pages/shell-page.ts'
import { expect, test } from '../fixtures.ts'

const cases: ReadonlyArray<{ language: Language; theme: Theme }> = [
  { language: 'en', theme: 'sentinel-dark' },
  { language: 'en', theme: 'sentinel-light' },
  { language: 'en', theme: 'monokai' },
  { language: 'en', theme: 'nord' },
  { language: 'es', theme: 'sentinel-dark' },
]

/**
 * Visual regression of the application shell against versioned baselines.
 *
 * Scenarios:
 * - the sites tree is masked because the tests that run beside this one fill it with their own collections
 * - the shell in the default theme in English and Spanish, and in the light, Monokai and Nord themes
 *
 * Baselines are generated and compared only inside the official Playwright container.
 */
test.describe('visual', () => {
  for (const { language, theme } of cases) {
    test(`shell in ${language} with the ${theme} theme`, async ({ shell, page }) => {
      await shell.setLanguage(language)
      await shell.setTheme(theme)

      await expect(page).toHaveScreenshot(`shell-${language}-${theme}.png`, {
        animations: 'disabled',
        mask: [page.getByRole('tree')],
      })
    })
  }
})
