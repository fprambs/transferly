import { createAccount } from '../accounts.ts'
import { expect, test } from '../fixtures.ts'

/** Master password of the vault of the account the test creates. */
const masterPassword = 'a long vault master password'

/**
 * Unlocking the vault from what needed it, against a real server. The test works with an account of its own, so locking its vault never
 * touches the account the other tests share.
 *
 * Scenarios:
 * - opening a saved site while the vault is locked asks for the master password at once, naming the site, its server and its user, and
 *   the folder lists once it is given, without pressing anything else
 */
test.describe('vault', () => {
  test('asks for the master password as soon as a saved site needs it', async ({ request, playwright, browser, baseURL }) => {
    const stamp = Date.now()
    const guest = await playwright.request.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    await createAccount(request, guest, `vault-${stamp}@example.com`, `Vault team ${stamp}`)
    const me = (await (await guest.get('/api/v1/me')).json()) as { csrfToken: string }
    const headers = { 'X-CSRF-Token': me.csrfToken }
    expect((await guest.post('/api/v1/vault/setup', { headers, data: { masterPassword } })).ok()).toBe(true)
    const workspaces = (await (await guest.get('/api/v1/workspaces')).json()) as Array<{ id: string; kind: string }>
    const personal = workspaces.find((workspace) => workspace.kind === 'personal') as { id: string }
    const name = `Locked ${stamp}`
    const created = await guest.post(`/api/v1/workspaces/${personal.id}/sites`, {
      headers,
      data: { name, protocol: 'sftp', host: 'sftp', port: 22, username: 'transferly', authMethod: 'password', password: 'transferly' },
    })
    expect(created.status()).toBe(201)
    const siteId = ((await created.json()) as { id: string }).id
    expect((await guest.post('/api/v1/vault/lock', { headers })).ok()).toBe(true)

    const context = await browser.newContext({ baseURL, storageState: await guest.storageState() })
    const page = await context.newPage()
    await page.goto(`/sites/${siteId}?path=%2F`)
    const unlock = page.getByRole('dialog', { name: 'Unlock the vault' })
    await expect(unlock).toBeVisible()
    await expect(unlock).toContainText(name)
    await expect(unlock).toContainText('sftp')
    await expect(unlock).toContainText('transferly')
    await expect(unlock.getByLabel('Master password')).toBeFocused()
    await page.keyboard.type(masterPassword)
    await page.keyboard.press('Enter')

    await page.getByRole('button', { name: 'Trust and connect' }).click()
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()
    await context.close()
    await guest.dispose()
  })
})
