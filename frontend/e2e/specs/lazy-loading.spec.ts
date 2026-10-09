import { expect, test } from '../fixtures.ts'

/** Checks that an unavailable feature chunk leaves navigation intact and recovers after a full reload. */
test.describe('deferred feature loading', () => {
  test('keeps settings and the shell available when a chunk fails, then recovers', async ({ page }) => {
    const chunk = '**/assets/SettingsPage-*.js'
    await page.route(chunk, (route) => route.abort())
    await page.goto('/?settings=general')
    const settings = page.getByRole('dialog', { name: 'Settings' })
    await expect(settings.getByRole('alert')).toContainText('This feature could not be loaded')
    await expect(page.locator('header[aria-label="Main toolbar"]')).toBeVisible()
    await page.unroute(chunk)
    await settings.getByRole('button', { name: 'Reload page' }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'General' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog', { name: 'Settings' })).toBeHidden()
    await expect(page.getByRole('heading', { level: 1, name: 'Transferly' })).toBeVisible()
  })
})
