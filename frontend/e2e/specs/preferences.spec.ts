import { expect, test } from '../fixtures.ts'

/**
 * Persisted interface preferences.
 *
 * Scenarios:
 * - choosing a dark theme applies its colors and survives a reload
 * - choosing a light theme removes the dark class
 * - following the system wears the light theme when the browser prefers light
 * - choosing Spanish translates the interface and survives a reload
 * - the theme is saved to the account: it comes back when the browser's own storage is empty
 * - the transfer queue can be collapsed and expanded
 */
test.describe('preferences', () => {
  test.describe('with the real account', () => {
    test.use({ accountTheme: 'real' })
    test.describe.configure({ mode: 'serial' })

    test('brings the theme back from the account when the browser storage is empty', async ({ shell, page }) => {
      await shell.setTheme('nord')
      await page.evaluate(() => globalThis.localStorage.clear())

      await page.reload()

      await expect(page.locator('html')).toHaveAttribute('data-theme', 'nord')

      await shell.setTheme('sentinel-dark')
    })
  })

  test('keeps the chosen dark theme after a reload', async ({ shell, page }) => {
    await shell.setTheme('monokai')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'monokai')
    await expect(page.locator('html')).toHaveClass(/dark/)
    await expect(page.locator('html')).toHaveCSS('--primary', '#a6e22e')

    await page.reload()

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'monokai')
    await page.goto('/settings')
    await expect(page.locator('[data-theme-option="monokai"]')).toHaveAttribute('aria-checked', 'true')
  })

  test('wears a light theme without the dark class', async ({ shell, page }) => {
    await shell.setTheme('github-light')

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'github-light')
    await expect(page.locator('html')).not.toHaveClass(/dark/)
  })

  test('follows a light operating system when asked to', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('/settings')
    await page.getByRole('checkbox', { name: 'Follow the system' }).check()

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'sentinel-light')
    await expect(page.locator('[data-theme-option="monokai"]')).toBeDisabled()
  })

  test('keeps Spanish after a reload', async ({ shell, page }) => {
    await shell.setLanguage('es')
    await expect(page.getByRole('region', { name: 'Transferencias' })).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('lang', 'es')

    await page.reload()

    await expect(page.getByRole('region', { name: 'Transferencias' })).toBeVisible()
  })

  test('collapses and expands the transfer queue', async ({ page, shell }) => {
    await page.routeWebSocket(/\/api\/v1\/ws/, (socket) => socket.send(JSON.stringify({ type: 'snapshot', data: { transfers: [] } })))
    await page.reload()
    await expect(shell.heading).toBeVisible()
    const body = page.locator('#queue-body')
    await expect(body).toBeVisible()

    await page.getByRole('button', { name: 'Hide transfers' }).click()
    await expect(body).toBeHidden()

    await page.getByRole('button', { name: 'Show transfers' }).click()
    await expect(body).toBeVisible()
  })
})
