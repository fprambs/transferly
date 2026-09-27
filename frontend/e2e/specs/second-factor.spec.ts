import { AxeBuilder } from '@axe-core/playwright'

import { expect, test } from '../fixtures.ts'
import { totpCode } from '../totp.ts'

/** Password of the test account, as the global setup creates it. */
const password = 'a long e2e password'

/**
 * The second factor against the production image, on the account the tests share, which turns it off at the end.
 *
 * Scenarios:
 * - the settings turn it on: the QR code and the key show, a wrong code is refused and a right one shows the recovery codes
 * - signing in from a clean browser then asks for a code after the password, and a right one opens the shell; wrong codes are covered by the
 *   unit tests, because the attempts of the account are limited and the tests share it
 * - a recovery code opens a session once and is refused the second time
 * - the settings section and the code step have no WCAG violations, and turning the factor off with the password restores the plain sign-in
 */
test.describe('second factor', () => {
  test.describe.configure({ mode: 'serial' })

  let secret = ''
  let recovery: string[] = []

  test('turns on from the settings', async ({ shell, page }) => {
    void shell
    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Settings' }).click()
    const section = page.getByRole('heading', { name: 'Two-step verification' }).locator('xpath=ancestor::section')
    await section.getByRole('button', { name: 'Turn on' }).click()

    const dialog = page.getByRole('dialog', { name: 'Turn on two-step verification' })
    await expect(dialog.getByRole('img', { name: /QR code/ })).toBeVisible()
    secret = (await dialog.locator('code').textContent()) ?? ''
    expect(secret).toMatch(/^[A-Z2-7]{32}$/)
    await dialog.getByLabel('Code from the app').fill('000000')
    await dialog.getByRole('button', { name: 'Confirm' }).click()
    await expect(dialog.getByRole('alert')).toContainText('The code is not right.')

    await dialog.getByLabel('Code from the app').fill(totpCode(secret))
    await dialog.getByRole('button', { name: 'Confirm' }).click()
    const codes = page.getByRole('dialog', { name: 'Save your recovery codes' })
    await expect(codes.getByRole('listitem')).toHaveCount(10)
    recovery = await codes.getByRole('listitem').allTextContents()
    await codes.getByRole('button', { name: 'I have saved them' }).click()
    await expect(section.getByText('10 recovery codes left')).toBeVisible()
  })

  test('asks for the code after the password', async ({ browser, baseURL }) => {
    const context = await browser.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    const guest = await context.newPage()
    await guest.goto('/login')
    await guest.getByLabel('Email').fill('e2e@example.com')
    await guest.getByLabel('Password').fill(password)
    await guest.getByRole('button', { name: 'Sign in' }).click()

    await expect(guest.getByLabel('Code')).toBeVisible()
    expect((await new AxeBuilder({ page: guest }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations).toEqual([])
    await guest.getByLabel('Code').fill(totpCode(secret, 1))
    await guest.getByRole('button', { name: 'Verify' }).click()
    await expect(guest.getByRole('button', { name: 'Account' })).toBeVisible()
    await context.close()
  })

  test('accepts a recovery code once', async ({ request }) => {
    const login = await request.post('/api/v1/auth/login', { data: { email: 'e2e@example.com', password } })
    expect(login.status()).toBe(202)
    const { challenge } = (await login.json()) as { challenge: string }

    const first = await request.post('/api/v1/auth/mfa', { data: { challenge, code: recovery[0] } })
    expect(first.status()).toBe(200)
    const second = await request.post('/api/v1/auth/mfa', { data: { challenge, code: recovery[0] } })
    expect(second.status()).toBe(401)
    expect(await second.json()).toMatchObject({ code: 'invalid_code' })
  })

  test('has an accessible section and turns off with the password', async ({ shell, page }) => {
    void shell
    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Settings' }).click()
    const section = page.getByRole('heading', { name: 'Two-step verification' }).locator('xpath=ancestor::section')
    await expect(section.getByText('On', { exact: true })).toBeVisible()
    expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations).toEqual([])

    await section.getByRole('button', { name: 'Turn off' }).click()
    const dialog = page.getByRole('dialog', { name: 'Turn off two-step verification' })
    await dialog.getByLabel('Account password').fill(password)
    await dialog.getByRole('button', { name: 'Turn off' }).click()
    await expect(section.getByText('Off', { exact: true })).toBeVisible()
  })
})
