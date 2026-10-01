import type { Language, Theme } from '../pages/shell-page.ts'
import { expect, test } from '../fixtures.ts'

const cases: ReadonlyArray<{ language: Language; theme: Theme }> = [
  { language: 'en', theme: 'transferly-dark' },
  { language: 'en', theme: 'transferly-light' },
  { language: 'en', theme: 'monokai' },
  { language: 'en', theme: 'nord' },
  { language: 'es', theme: 'transferly-dark' },
]

/**
 * Visual regression of the application shell against versioned baselines.
 *
 * Scenarios:
 * - the sites sidebar is masked because the tests that run beside this one fill it with their own collections, and the event
 *   stream and the transfer history are replaced by empty ones so their transfers do not change the dock
 * - the settings page in the default dark theme and the default light theme
 * - the shell in the default theme in English and Spanish, and in the light, Monokai and Nord themes
 *
 * Baselines are generated and compared only inside the official Playwright container.
 */
test.describe('visual', () => {
  test.beforeEach(async ({ page }) => {
    await page.routeWebSocket(/\/api\/v1\/ws/, (socket) => socket.send(JSON.stringify({ type: 'snapshot', data: { transfers: [] } })))
    await page.route('**/api/v1/transfers*', (route) => route.fulfill({ json: [] }))
  })

  for (const { language, theme } of cases) {
    test(`shell in ${language} with the ${theme} theme`, async ({ shell, page }) => {
      await shell.setLanguage(language)
      await shell.setTheme(theme)
      const dock = page.getByRole('region', { name: /^(Transfers|Transferencias)$/ })
      await expect(dock).toBeVisible()
      await expect(page.getByRole('heading', { level: 2, name: /^(Pick a site to start|Elige un sitio para empezar)$/ })).toBeVisible()

      await expect(page).toHaveScreenshot(`shell-${language}-${theme}.png`, {
        animations: 'disabled',
        mask: [page.getByRole('navigation', { name: /^(Sites|Sitios)$/ })],
      })
    })
  }

  for (const theme of ['transferly-dark', 'transferly-light'] as const) {
    test(`settings in en with the ${theme} theme`, async ({ shell, page }) => {
      await shell.setTheme(theme)
      await page.goto('/settings')
      await expect(page.getByRole('radiogroup')).toBeVisible()

      await expect(page).toHaveScreenshot(`settings-en-${theme}.png`, { animations: 'disabled', mask: [page.getByRole('navigation', { name: 'Sites' })] })
    })
  }
})
