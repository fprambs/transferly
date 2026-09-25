import { expect, test } from '../fixtures.ts'

/**
 * Persisted interface preferences.
 *
 * Scenarios:
 * - choosing a dark theme applies its colors and survives a reload
 * - choosing a light theme removes the dark class
 * - following the system wears the light theme when the browser prefers light
 * - choosing Spanish translates the interface and survives a reload
 * - the transfer queue can be collapsed and expanded
 */
test.describe('preferences', () => {
  test('keeps the chosen dark theme after a reload', async ({ shell, page }) => {
    await shell.setTheme('monokai')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'monokai')
    await expect(page.locator('html')).toHaveClass(/dark/)
    await expect(page.locator('html')).toHaveCSS('--primary', '#a6e22e')

    await page.reload()

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'monokai')
    await expect(shell.themeSelect).toHaveValue('monokai')
  })

  test('wears a light theme without the dark class', async ({ shell, page }) => {
    await shell.setTheme('github-light')

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'github-light')
    await expect(page.locator('html')).not.toHaveClass(/dark/)
  })

  test('follows a light operating system when asked to', async ({ page, shell }) => {
    await page.emulateMedia({ colorScheme: 'light' })
    await page.getByRole('checkbox', { name: 'Follow the system' }).check()

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'sentinel-light')
    await expect(shell.themeSelect).toBeDisabled()
  })

  test('keeps Spanish after a reload', async ({ shell, page }) => {
    await shell.setLanguage('es')
    await expect(page.getByRole('region', { name: 'Transferencias' })).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('lang', 'es')

    await page.reload()

    await expect(page.getByRole('region', { name: 'Transferencias' })).toBeVisible()
  })

  test('collapses and expands the transfer queue', async ({ page, shell }) => {
    await expect(shell.heading).toBeVisible()
    await expect(page.getByText('No transfers')).toBeVisible()

    await page.getByRole('button', { name: 'Hide transfers' }).click()
    await expect(page.getByText('No transfers')).toBeHidden()

    await page.getByRole('button', { name: 'Show transfers' }).click()
    await expect(page.getByText('No transfers')).toBeVisible()
  })
})
