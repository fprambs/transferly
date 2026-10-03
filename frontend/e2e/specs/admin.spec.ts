import { AxeBuilder } from '@axe-core/playwright'
import type { APIRequestContext, Page } from '@playwright/test'

import { createAccountWithSecondStep } from '../accounts.ts'
import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'
import { settle } from '../settle.ts'

/** Opens the administration in the settings dialog from the account menu and waits for its accounts. */
async function openAdmin(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Account' }).click()
  await page.getByRole('menuitem', { name: 'Settings' }).click()
  await page.getByRole('navigation', { name: 'Settings' }).getByRole('button', { name: 'Administration', exact: true }).click()
  await expect(page).toHaveURL(/settings=admin/)
  await expect(page.getByRole('table', { name: 'Accounts' })).toBeVisible()
}

/** The row of an account in the accounts table. */
function rowOf(page: Page, email: string) {
  return page.getByRole('table', { name: 'Accounts' }).locator('tbody tr').filter({ hasText: email })
}

/** The networks an administrator added, as the table lists them. */
function addedNetworks(page: Page) {
  return page.getByRole('table', { name: 'Allowed private networks' }).locator('tbody tr').filter({ hasText: 'Added by an administrator' })
}

/** Types a network in the field under the table and adds it to the list. */
async function addNetwork(page: Page, network: string): Promise<void> {
  await page.getByLabel('Network to add').fill(network)
  await page.getByRole('button', { name: 'Add', exact: true }).click()
}

/** Runs the accessibility rules against the page and expects no violations. */
async function expectAccessible(page: Page, label: string): Promise<void> {
  await settle(page)
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(results.violations, label).toEqual([])
}

/**
 * Administration of the instance against the production image, as the administrator the global setup created.
 *
 * Scenarios:
 * - the settings of the account menu offer Administration, which lists the accounts, among them one created through an invitation whose second step is on
 * - making the account an administrator and back changes its role in the list
 * - removing its second step asks first, and afterwards the list shows it off
 * - disabling it asks first, ends its session at once, and enabling it makes it active again without a question
 * - the networks tab adds entries to its table, saves them, keeps them after a reload, refuses the whole internet and clears the list
 * - the limits page saves values in its units, keeps them after a reload, refuses what is not a number and goes back to the starting values
 * - the audit log words each change and narrows by group
 * - the connectors tab shows SFTP and FTP offered, and stopping to offer one asks first and can be canceled without a change
 * - the five tabs and the confirmations have no WCAG violations in the light and dark themes
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
    await expect(page.getByRole('table', { name: 'Allowed private networks' })).toContainText('No network was added yet.')
    await addNetwork(page, '0.0.0.0/0')
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('alert')).toContainText('not a network that can be allowed')

    await page.getByRole('button', { name: 'Remove 0.0.0.0/0' }).click()
    await addNetwork(page, '192.168.77.0/24')
    await addNetwork(page, '10.77.0.5')
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('status')).toContainText('Saved.')
    await page.reload()
    await page.getByRole('tab', { name: 'Networks' }).click()
    await expect(addedNetworks(page)).toHaveText([/10\.77\.0\.5\/32/, /192\.168\.77\.0\/24/])

    await page.getByRole('button', { name: 'Remove 10.77.0.5/32' }).click()
    await page.getByRole('button', { name: 'Remove 192.168.77.0/24' }).click()
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('status')).toBeVisible()
    await expect(addedNetworks(page)).toHaveCount(0)
  })

  test('saves and restores the limits', async ({ shell, page }) => {
    void shell
    await page.goto('/admin')
    await page.getByRole('tab', { name: 'Limits' }).click()
    const sites = page.getByLabel('Sites per workspace')
    const bandwidth = page.getByLabel('Per person', { exact: true }).first()
    await expect(sites).toHaveValue('')
    await sites.fill('lots')
    await page.getByRole('button', { name: 'Save limits' }).click()
    await expect(page.getByRole('alert')).toContainText('A limit is not valid')

    await sites.fill('500')
    await bandwidth.fill('200')
    await page.getByRole('button', { name: 'Save limits' }).click()
    await expect(page.getByRole('status')).toContainText('Saved.')
    await page.reload()
    await page.getByRole('tab', { name: 'Limits' }).click()
    await expect(page.getByLabel('Sites per workspace')).toHaveValue('500')
    await expect(page.getByLabel('Per person', { exact: true }).first()).toHaveValue('200')

    await page.getByRole('button', { name: 'Fill in the starting values' }).click()
    await page.getByRole('button', { name: 'Save limits' }).click()
    await expect(page.getByRole('status')).toContainText('Saved.')
    await expect(page.getByLabel('Sites per workspace')).toHaveValue('')
  })

  test('records what happened in the audit log', async ({ shell, page }) => {
    void shell
    await page.goto('/admin')
    await page.getByRole('tab', { name: 'Audit log' }).click()
    const log = page.getByRole('table', { name: 'Instance audit log' })
    await expect(log).toContainText(`removed the second step of ${email}`)
    await expect(log).toContainText(`disabled the account ${email}`)
    await expect(log).toContainText(`enabled the account ${email}`)
    await expect(log).toContainText(`made ${email} Administrator`)
    await expect(log).toContainText('changed the allowed networks (2 networks)')
    await expect(log).toContainText('changed the limits of the instance')
    await expect(log.locator('tbody tr').first()).toContainText('e2e@example.com')

    await choose(page.getByLabel('Show'), 'Networks')
    await expect(log).not.toContainText('disabled the account')
    await expect(log).toContainText('changed the allowed networks')
  })

  test('shows the connectors and asks before it stops offering one', async ({ shell, page }) => {
    void shell
    await openAdmin(page)
    await page.getByRole('tab', { name: 'Connectors' }).click()
    const sftp = page.getByRole('listitem', { name: 'SFTP' })
    await expect(sftp).toContainText('Offered')
    await expect(page.getByRole('listitem', { name: 'FTP / FTPS' })).toContainText('Offered')

    await sftp.getByRole('switch', { name: 'Offer SFTP' }).click()
    const question = page.getByRole('dialog', { name: 'Stop offering SFTP?' })
    await expect(question).toBeVisible()
    await expectAccessible(page, 'connector question')
    await question.getByRole('button', { name: 'Cancel' }).click()
    await expect(question).toBeHidden()
    await expect(sftp.getByRole('switch', { name: 'Offer SFTP' })).toBeChecked()
  })

  test('has no WCAG violations in the light and dark themes', async ({ shell, page }) => {
    test.setTimeout(120_000)
    for (const theme of ['transferly-dark', 'transferly-light']) {
      await page.goto('/')
      await shell.setTheme(theme)
      await openAdmin(page)
      await expect(page.getByRole('table', { name: 'Accounts' })).toBeVisible()
      await expectAccessible(page, `${theme} accounts`)
      await settle(page)
      const networksTab = page.getByRole('tab', { name: 'Networks' })
      await networksTab.scrollIntoViewIfNeeded()
      await expect(networksTab).toBeEnabled()
      await networksTab.click()
      await expect(page.getByLabel('Network to add')).toBeVisible()
      await expectAccessible(page, `${theme} networks`)
      await settle(page)
      await page.getByRole('tab', { name: 'Limits' }).scrollIntoViewIfNeeded()
      await page.getByRole('tab', { name: 'Limits' }).click()
      await expect(page.getByLabel('Sites per workspace')).toBeVisible()
      await expectAccessible(page, `${theme} limits`)
      await settle(page)
      await page.getByRole('tab', { name: 'Connectors' }).scrollIntoViewIfNeeded()
      await page.getByRole('tab', { name: 'Connectors' }).click()
      await expect(page.getByRole('switch', { name: 'Offer SFTP' })).toBeVisible()
      await expectAccessible(page, `${theme} connectors`)
      await settle(page)
      await page.getByRole('tab', { name: 'Audit log' }).scrollIntoViewIfNeeded()
      await page.getByRole('tab', { name: 'Audit log' }).click()
      await expect(page.getByRole('heading', { name: 'Instance audit log' })).toBeVisible()
      await expect(page.getByText('Loading the audit log…')).toBeHidden({ timeout: 15_000 })
      await expectAccessible(page, `${theme} audit`)
    }
  })
})
