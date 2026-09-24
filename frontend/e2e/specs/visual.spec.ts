import type { Language, Theme } from '../pages/shell-page.ts'
import { expect, test } from '../fixtures.ts'

const cases: ReadonlyArray<{ language: Language; theme: Theme }> = [
  { language: 'en', theme: 'light' },
  { language: 'en', theme: 'dark' },
  { language: 'es', theme: 'light' },
  { language: 'es', theme: 'dark' },
]

/**
 * Visual regression of the application shell against versioned baselines.
 *
 * Scenarios:
 * - the shell in English and Spanish, each in the light and dark theme
 *
 * Baselines are generated and compared only inside the official Playwright container.
 */
test.describe('visual', () => {
  for (const { language, theme } of cases) {
    test(`shell in ${language} with the ${theme} theme`, async ({ shell, page }) => {
      await shell.setLanguage(language)
      await shell.setTheme(theme)

      await expect(page).toHaveScreenshot(`shell-${language}-${theme}.png`, { animations: 'disabled' })
    })
  }
})
