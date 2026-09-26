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
 * - the sites sidebar and the transfers dock are masked because the tests that run beside this one fill them with their own
 *   collections and transfers
 * - the settings page in the default dark theme and the default light theme
 * - the shell in the default theme in English and Spanish, and in the light, Monokai and Nord themes
 *
 * Baselines are generated and compared only inside the official Playwright container.
 */
test.describe('visual', () => {
  for (const { language, theme } of cases) {
    test(`shell in ${language} with the ${theme} theme`, async ({ shell, page }) => {
      await shell.setLanguage(language)
      await shell.setTheme(theme)
      const dock = page.getByRole('region', { name: /^(Transfers|Transferencias)$/ })
      await expect(dock).toBeVisible()
      await expect(page.getByRole('heading', { level: 2, name: /^(Pick a site to start|Elige un sitio para empezar)$/ })).toBeVisible()

      await expect(page).toHaveScreenshot(`shell-${language}-${theme}.png`, {
        animations: 'disabled',
        mask: [page.getByRole('navigation', { name: 'Sites' }), dock],
      })
    })
  }

  for (const theme of ['sentinel-dark', 'sentinel-light'] as const) {
    test(`settings in en with the ${theme} theme`, async ({ shell, page }) => {
      await shell.setTheme(theme)
      await page.goto('/settings')
      await expect(page.getByRole('radiogroup')).toBeVisible()

      await expect(page).toHaveScreenshot(`settings-en-${theme}.png`, { animations: 'disabled', mask: [page.getByRole('navigation', { name: 'Sites' })] })
    })
  }
})
