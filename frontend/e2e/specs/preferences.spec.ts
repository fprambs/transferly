import { expect, test } from '../fixtures.ts'

/**
 * Persisted interface preferences.
 *
 * Scenarios:
 * - choosing the dark theme applies it and survives a reload
 * - choosing Spanish translates the interface and survives a reload
 * - the transfer queue can be collapsed and expanded
 */
test.describe('preferences', () => {
  test('keeps the dark theme after a reload', async ({ shell, page }) => {
    await shell.setTheme('dark')
    await expect(page.locator('html')).toHaveClass(/dark/)

    await page.reload()

    await expect(page.locator('html')).toHaveClass(/dark/)
    await expect(shell.themeGroup.getByRole('button', { name: 'Dark' })).toHaveAttribute('aria-pressed', 'true')
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
