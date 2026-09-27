import { AxeBuilder } from '@axe-core/playwright'
import type { APIRequestContext, Page } from '@playwright/test'

import { createAccountWithSecondStep } from '../accounts.ts'
import { expect, test } from '../fixtures.ts'

/** Opens the administration page from the account menu and waits for its accounts. */
async function openAdmin(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Account' }).click()
  await page.getByRole('menuitem', { name: 'Administration' }).click()
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByRole('list', { name: 'Accounts' })).toBeVisible()
}

/** The row of an account in the accounts table. */
function rowOf(page: Page, email: string) {
  return page.getByRole('list', { name: 'Accounts' }).getByRole('listitem').filter({ hasText: email })
}

/** Runs the accessibility rules against the page and expects no violations. */
async function expectAccessible(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(results.violations, label).toEqual([])
}

/**
 * Administration of the instance against the production image, as the administrator the global setup created.
 *
 * Scenarios:
 * - the account menu offers Administration, and the page lists the accounts, among them one created through an invitation whose second step is on
 * - making the account an administrator and back changes its role in the list
 * - removing its second step asks first, and afterwards the list shows it off
 * - disabling it asks first, ends its session at once, and enabling it makes it active again without a question
 * - the networks page saves an entry, keeps it after a reload, refuses the whole internet and clears the list
 * - the audit log words each change and narrows by group
 * - the three tabs and the confirmation have no WCAG violations in the light and dark themes
 */
test.describe('administration', () => {
  test.describe.configure({ mode: 'serial' })

  const stamp = Date.now()
  const email = `e2e-target-${stamp}@example.com`
  let guest: APIRequestContext

  test.beforeAll(async ({ playwright, baseURL }) => {
    guest = await playwright.request.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  })

  test.afterAll(async () => {
    await guest.dispose()
  })

  test('lists the accounts', async ({ shell, page, request }) => {
    void shell
    await createAccountWithSecondStep(request, guest, email, `e2e-admin-team-${stamp}`)
    await openAdmin(page)

    const row = rowOf(page, email)
    await expect(row).toContainText('User')
    await expect(row).toContainText('Active')
    await expect(row).toContainText('On')
    await expect(rowOf(page, 'e2e@example.com')).toContainText('You')
    await expect(page.getByRole('button', { name: 'Actions for e2e@example.com' })).toHaveCount(0)
  })

  test('changes the role', async ({ shell, page }) => {
    void shell
    await page.goto('/admin')
    const row = rowOf(page, email)
    await page.getByRole('button', { name: `Actions for ${email}` }).click()
    await page.getByRole('menuitem', { name: 'Make administrator' }).click()
    await expect(row).toContainText('Administrator')
    await page.getByRole('button', { name: `Actions for ${email}` }).click()
    await page.getByRole('menuitem', { name: 'Make regular user' }).click()
    await expect(row).toContainText('User')
  })

  test('removes the second step after asking', async ({ shell, page }) => {
    void shell
    await page.goto('/admin')
    await page.getByRole('button', { name: `Actions for ${email}` }).click()
    await page.getByRole('menuitem', { name: 'Remove second step' }).click()
    const dialog = page.getByRole('dialog', { name: `Remove the second step of ${email}?` })
    await expectAccessible(page, 'confirmation')
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(rowOf(page, email)).toContainText('On')

    await page.getByRole('button', { name: `Actions for ${email}` }).click()
    await page.getByRole('menuitem', { name: 'Remove second step' }).click()
    await page.getByRole('dialog', { name: `Remove the second step of ${email}?` }).getByRole('button', { name: 'Remove second step' }).click()
    await expect(rowOf(page, email)).toContainText('Off')
    const security = (await (await guest.get('/api/v1/me/security')).json()) as { totp: boolean }
    expect(security.totp).toBe(false)
  })

  test('disables and enables the account', async ({ shell, page }) => {
    void shell
    await page.goto('/admin')
    await page.getByRole('button', { name: `Actions for ${email}` }).click()
    await page.getByRole('menuitem', { name: 'Disable account' }).click()
    await page.getByRole('dialog', { name: `Disable ${email}?` }).getByRole('button', { name: 'Disable account' }).click()
    await expect(rowOf(page, email)).toContainText('Disabled')
    expect((await guest.get('/api/v1/me')).status()).toBe(401)

    await page.getByRole('button', { name: `Actions for ${email}` }).click()
    await page.getByRole('menuitem', { name: 'Enable account' }).click()
    await expect(rowOf(page, email)).toContainText('Active')
  })

  test('saves and clears the networks', async ({ shell, page }) => {
    void shell
    await page.goto('/admin')
    await page.getByRole('tab', { name: 'Networks' }).click()
    const box = page.getByLabel('Added by administrators')
    await expect(box).toHaveValue('')
    await box.fill('0.0.0.0/0')
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('alert')).toContainText('not a network that can be allowed')

    await box.fill('192.168.77.0/24\n\n10.77.0.5')
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('status')).toContainText('Saved.')
    await page.reload()
    await page.getByRole('tab', { name: 'Networks' }).click()
    await expect(page.getByLabel('Added by administrators')).toHaveValue('10.77.0.5/32\n192.168.77.0/24')

    await page.getByLabel('Added by administrators').fill('')
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('status')).toBeVisible()
    await expect(page.getByLabel('Added by administrators')).toHaveValue('')
  })

  test('records what happened in the audit log', async ({ shell, page }) => {
    void shell
    await page.goto('/admin')
    await page.getByRole('tab', { name: 'Audit log' }).click()
    const log = page.getByRole('list', { name: 'Instance audit log' })
    await expect(log).toContainText(`removed the second step of ${email}`)
    await expect(log).toContainText(`disabled the account ${email}`)
    await expect(log).toContainText(`enabled the account ${email}`)
    await expect(log).toContainText(`made ${email} Administrator`)
    await expect(log).toContainText('changed the allowed networks (2 networks)')
    await expect(log.getByRole('listitem').first()).toContainText('e2e@example.com')

    await page.getByLabel('Show').selectOption({ label: 'Networks' })
    await expect(log).not.toContainText('disabled the account')
    await expect(log).toContainText('changed the allowed networks')
  })

  test('has no WCAG violations in the light and dark themes', async ({ shell, page }) => {
    for (const theme of ['sentinel-dark', 'sentinel-light']) {
      await page.goto('/')
      await shell.setTheme(theme)
      await page.goto('/admin')
      await expect(page.getByRole('list', { name: 'Accounts' })).toBeVisible()
      await expectAccessible(page, `${theme} accounts`)
      await page.getByRole('tab', { name: 'Networks' }).click()
      await expect(page.getByLabel('Added by administrators')).toBeVisible()
      await expectAccessible(page, `${theme} networks`)
      await page.getByRole('tab', { name: 'Audit log' }).click()
      await expect(page.getByRole('list', { name: 'Instance audit log' })).toBeVisible()
      await expectAccessible(page, `${theme} audit`)
    }
  })
})
