import type { Page } from '@playwright/test'

import { createAccountWithSecondStep } from '../accounts.ts'
import { expect, test } from '../fixtures.ts'
import { totpCode } from '../totp.ts'

/** Where the screenshots are written, relative to the frontend folder. */
const folder = 'shots'

/** Saves a screenshot of the page under a numbered name. */
async function shoot(page: Page, name: string): Promise<void> {
  await page.screenshot({ path: `${folder}/${name}.png` })
}

/** Reads the anti-CSRF token of the signed-in session. */
async function csrfOf(page: Page): Promise<string> {
  return ((await (await page.request.get('/api/v1/me')).json()) as { csrfToken: string }).csrfToken
}

/** Gives the page a virtual authenticator that answers passkey requests at once. */
async function addVirtualAuthenticator(page: Page): Promise<void> {
  const client = await page.context().newCDPSession(page)
  await client.send('WebAuthn.enable')
  await client.send('WebAuthn.addVirtualAuthenticator', {
    options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  })
}

/**
 * Takes the screenshots of the team screens and of the vault notice, to review the interface without opening it.
 * It is skipped unless `SENTINEL_E2E_SCREENSHOTS` is set, so the normal run does not write files.
 *
 * Scenarios:
 * - the team switcher, the new team dialog, the members page, the invite dialog in both steps, the invitations tab, the invitation page
 *   for a new account and for the wrong account, the members page of a viewer, the row menu and the removal dialog
 * - the passkey dialog, the settings with a passkey and the sign-in step that offers it
 * - the administration page: accounts, the confirmation to remove a second step, networks and the audit log
 * - the locked vault in a new site, the unlock dialog, a saved site that asks for the master password at once naming its server, and a quick connection that works while locked
 * - comparing a saved FTP site against the SFTP quick connection: the differences and the plan running
 * - searching a folder recursively: the results found below a subfolder
 * - editing a site's speed limit
 * - editing a site's proxy and jump host
 * - the remote editor
 * - the site dialog on its General and Advanced tabs, with an anonymous FTP site and a site that asks for its password, and that site asking for it
 * - a folder with many files, with the transfers panel still in view
 * - a tab that is still loading, one that failed to load and offers to retry, a ZIP download with its card and in the transfers panel, and the double click setting
 */
test.describe('screenshots', () => {
  test.skip(process.env.SENTINEL_E2E_SCREENSHOTS === undefined, 'Only run to take the screenshots.')
  test.use({ viewport: { width: 1280, height: 820 } })

  test('teams and the vault', async ({ shell, page, quickId, browser, baseURL }) => {
    void shell
    test.setTimeout(180_000)
    const stamp = Date.now()
    const team = `Acme Web ${stamp % 1000}`
    const invitee = `ana-${stamp}@acme.dev`

    await page.getByRole('button', { name: 'Workspace' }).click()
    await expect(page.getByRole('menuitem', { name: 'New team workspace' })).toBeVisible()
    await shoot(page, '00-switcher')
    await page.getByRole('menuitem', { name: 'New team workspace' }).click()
    const dialog = page.getByRole('dialog', { name: 'New team workspace' })
    await dialog.getByLabel('Name').fill(team)
    await shoot(page, '01-new-team')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(page.getByRole('heading', { name: 'Members', exact: true })).toBeVisible()
    await shoot(page, '02-members-alone')

    await page.getByRole('button', { name: 'Invite member' }).click()
    const invite = page.getByRole('dialog', { name: `Invite to ${team}` })
    await invite.getByLabel('Email').fill(invitee)
    await invite.getByRole('radio', { name: /Viewer/ }).check()
    await shoot(page, '03-invite-form')
    await invite.getByRole('button', { name: 'Create invitation' }).click()
    const ready = page.getByRole('dialog', { name: 'Invitation ready' })
    const link = await ready.getByLabel('One-time link').inputValue()
    await shoot(page, '04-invite-link')
    await ready.getByRole('button', { name: 'Done' }).click()
    await page.getByRole('tab', { name: /Invitations/ }).click()
    await expect(page.getByRole('list', { name: 'Invitations' })).toContainText(invitee)
    await shoot(page, '05-invitations')

    const wrong = await page.context().newPage()
    await wrong.setViewportSize({ width: 1280, height: 820 })
    await wrong.goto(link)
    await expect(wrong.getByRole('alert')).toBeVisible()
    await shoot(wrong, '08-accept-wrong-account')
    await wrong.close()

    const context = await browser.newContext({ baseURL, viewport: { width: 1280, height: 820 }, storageState: { cookies: [], origins: [] } })
    const guest = await context.newPage()
    await guest.goto(link)
    await expect(guest.getByRole('heading', { name: `You were invited to ${team}` })).toBeVisible()
    await shoot(guest, '06-accept-new-account')
    await guest.getByLabel('Account password').fill('a long guest password')
    await guest.getByLabel('Repeat password').fill('a long guest password')
    await guest.getByRole('button', { name: 'Create account and join' }).click()
    await expect(guest.getByRole('list', { name: 'Members' }).getByRole('listitem')).toHaveCount(2)
    await shoot(guest, '07-members-as-viewer')
    await context.close()

    await page.reload()
    await page.getByRole('tab', { name: /Members/ }).click()
    await expect(page.getByRole('list', { name: 'Members' }).getByRole('listitem')).toHaveCount(2)
    await shoot(page, '09-members-with-a-viewer')
    await page.getByRole('button', { name: `Actions for ${invitee}` }).click()
    await expect(page.getByRole('menuitem', { name: 'Remove from team' })).toBeVisible()
    await shoot(page, '10-member-menu')
    await page.getByRole('menuitem', { name: 'Remove from team' }).click()
    await expect(page.getByRole('dialog', { name: `Remove ${invitee}?` })).toBeVisible()
    await shoot(page, '11-remove-dialog')
    await page.getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitem', { name: 'Audit log' }).click()
    await expect(page.getByRole('list', { name: 'Audit log' })).toContainText(`invited ${invitee}`)
    await shoot(page, '11b-audit-log')

    const csrf = await csrfOf(page)
    const workspaces = (await (await page.request.get('/api/v1/workspaces')).json()) as Array<{ id: string; kind: string }>
    const personal = workspaces.find((workspace) => workspace.kind === 'personal')?.id as string
    const saved = await page.request.post(`/api/v1/workspaces/${personal}/sites`, {
      headers: { 'X-CSRF-Token': csrf },
      data: { name: `Saved ${stamp % 1000}`, protocol: 'sftp', host: 'sftp', port: 22, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    expect(saved.status()).toBe(201)
    const siteId = ((await saved.json()) as { id: string }).id

    await page.request.post('/api/v1/vault/lock', { headers: { 'X-CSRF-Token': csrf } })
    await page.goto('/')
    await page.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitem', { name: /^Personal/ }).click()
    await expect(page.getByRole('button', { name: 'Vault locked' })).toBeVisible()
    await shoot(page, '12-vault-chip-locked')

    await page.getByRole('button', { name: 'New site' }).click()
    const site = page.getByRole('dialog', { name: 'New site' })
    await site.getByLabel('Name', { exact: true }).fill('Production')
    await site.getByLabel('Password').fill('secret')
    await expect(site.getByRole('status')).toBeVisible()
    await shoot(page, '13-site-vault-locked')
    await site.getByRole('button', { name: 'Unlock the vault' }).click()
    await expect(page.getByRole('dialog', { name: 'Unlock the vault' })).toBeVisible()
    await shoot(page, '14-unlock-in-place')
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Cancel' }).click()

    await page.goto(`/sites/${siteId}?path=%2F`)
    await expect(page.getByRole('dialog', { name: 'Unlock the vault' })).toContainText('sentinel')
    await shoot(page, '15-saved-site-asks-master-password')
    await page.getByRole('dialog', { name: 'Unlock the vault' }).getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await shoot(page, '15b-vault-locked-card-dark')
    await shell.setTheme('solarized-light')
    await page.goto(`/sites/${siteId}?path=%2F`)
    await page.getByRole('dialog', { name: 'Unlock the vault' }).getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await shoot(page, '15c-vault-locked-card-light')
    await shell.setTheme('sentinel-dark')

    await page.goto(`/sites/${quickId}?path=%2F`)
    await expect(page.getByRole('row').first()).toBeVisible()
    await shoot(page, '16-quick-connection-vault-locked')

    await page.goto(`/sites/${siteId}?path=%2F`)
    await page.getByRole('dialog', { name: 'Unlock the vault' }).getByLabel('Master password').fill('a long e2e master password')
    await page.getByRole('dialog', { name: 'Unlock the vault' }).getByRole('button', { name: 'Unlock' }).click()
    await expect(page.getByRole('row').first()).toBeVisible()
    await shoot(page, '17-saved-site-after-unlock')

    await page.goto('/')
    await page.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitem', { name: new RegExp(`^${team}`) }).click()
    await page.goto('/members')
    await shell.setLanguage('es')
    await page.goto('/members')
    await expect(page.getByRole('heading', { name: 'Miembros', exact: true })).toBeVisible()
    await shoot(page, '18-members-es')

    await shell.setLanguage('en')
    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Settings' }).click()
    const section = page.getByRole('heading', { name: 'Two-step verification' }).locator('xpath=ancestor::section')
    await section.scrollIntoViewIfNeeded()
    await shoot(page, '19-security-off')
    await section.getByRole('button', { name: 'Turn on' }).click()
    const enable = page.getByRole('dialog', { name: 'Turn on two-step verification' })
    await expect(enable.getByRole('img', { name: /QR code/ })).toBeVisible()
    await shoot(page, '20-enable-two-step')
    const secret = (await enable.locator('code').textContent()) ?? ''
    await enable.getByLabel('Code from the app').fill(totpCode(secret))
    await enable.getByRole('button', { name: 'Confirm' }).click()
    await expect(page.getByRole('dialog', { name: 'Save your recovery codes' })).toBeVisible()
    await shoot(page, '21-recovery-codes')
    await page.getByRole('button', { name: 'I have saved them' }).click()
    await section.scrollIntoViewIfNeeded()
    await shoot(page, '22-security-on')

    const clean = await browser.newContext({ baseURL, viewport: { width: 1280, height: 820 }, storageState: { cookies: [], origins: [] } })
    const login = await clean.newPage()
    await login.goto('/login')
    await login.getByLabel('Email').fill('e2e@example.com')
    await login.getByLabel('Password').fill('a long e2e password')
    await login.getByRole('button', { name: 'Sign in' }).click()
    await expect(login.getByLabel('Code')).toBeVisible()
    await shoot(login, '23-login-code-step')
    await clean.close()

    await section.getByRole('button', { name: 'Turn off' }).click()
    const off = page.getByRole('dialog', { name: 'Turn off two-step verification' })
    await off.getByLabel('Account password').fill('a long e2e password')
    await off.getByRole('button', { name: 'Turn off' }).click()
    await expect(section.getByText('Off', { exact: true })).toBeVisible()
  })

  test('passkeys', async ({ browser }) => {
    const password = 'a long e2e password'
    const context = await browser.newContext({ baseURL: 'http://passkeys.localhost:8080', viewport: { width: 1280, height: 820 }, storageState: { cookies: [], origins: [] } })
    const page = await context.newPage()
    await addVirtualAuthenticator(page)
    const signIn = async () => {
      await page.goto('/')
      await page.getByLabel('Email').fill('e2e@example.com')
      await page.getByLabel('Password').fill(password)
      await page.getByRole('button', { name: 'Sign in' }).click()
    }
    await signIn()
    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Settings' }).click()
    await page.getByRole('button', { name: 'Add a passkey' }).click()
    const dialog = page.getByRole('dialog', { name: 'Add a passkey' })
    await dialog.getByLabel('Name').fill('MacBook fingerprint')
    await shoot(page, '24-add-passkey')
    await dialog.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByRole('dialog', { name: 'Save your recovery codes' })).toBeVisible()
    await page.getByRole('button', { name: 'I have saved them' }).click()
    await page.getByRole('heading', { name: 'Passkeys' }).scrollIntoViewIfNeeded()
    await shoot(page, '25-security-passkeys')

    await context.clearCookies()
    await signIn()
    await expect(page.getByRole('button', { name: 'Use a passkey' })).toBeVisible()
    await shoot(page, '26-login-passkey-step')

    await page.getByRole('button', { name: 'Use a passkey' }).click()
    await expect(page.getByRole('button', { name: 'Account' })).toBeVisible()
    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Settings' }).click()
    await page.getByRole('button', { name: 'Remove the passkey MacBook fingerprint' }).click()
    const remove = page.getByRole('dialog', { name: 'Remove the passkey MacBook fingerprint' })
    await remove.getByLabel('Account password').fill(password)
    await remove.getByRole('button', { name: 'Remove' }).click()
    await expect(page.getByText('No passkeys yet.')).toBeVisible()
    await context.close()
  })

  test('administration', async ({ page, request, playwright, baseURL }) => {
    const stamp = Date.now()
    const email = `ana.perez-${stamp}@acme.dev`
    const guest = await playwright.request.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    await createAccountWithSecondStep(request, guest, email, `Shots team ${stamp}`)
    await page.goto('/')
    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Administration' }).click()
    await expect(page.getByRole('list', { name: 'Accounts' })).toBeVisible()
    await shoot(page, '27-admin-accounts')

    await page.getByRole('button', { name: `Actions for ${email}` }).click()
    await shoot(page, '28-admin-account-menu')
    await page.getByRole('menuitem', { name: 'Remove second step' }).click()
    await expect(page.getByRole('dialog', { name: `Remove the second step of ${email}?` })).toBeVisible()
    await shoot(page, '29-admin-reset-confirm')
    await page.getByRole('dialog').getByRole('button', { name: 'Remove second step' }).click()
    await expect(page.getByRole('list', { name: 'Accounts' }).getByRole('listitem').filter({ hasText: email })).toContainText('Off')

    await page.getByRole('tab', { name: 'Networks' }).click()
    await page.getByLabel('Added by administrators').fill('192.168.1.0/24\n10.20.0.5')
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('status')).toBeVisible()
    await shoot(page, '30-admin-networks')

    await page.getByRole('tab', { name: 'Limits' }).click()
    await expect(page.getByLabel('Sites per workspace')).toBeVisible()
    await page.getByLabel('Per person', { exact: true }).first().fill('2')
    await page.getByLabel('Sites per workspace').fill('50')
    await page.getByLabel('Volume per person and day').fill('5')
    await shoot(page, '32-admin-limits')

    await page.getByRole('tab', { name: 'Audit log' }).click()
    await expect(page.getByRole('list', { name: 'Instance audit log' })).toContainText('changed the allowed networks')
    await shoot(page, '31-admin-audit')

    await page.getByRole('tab', { name: 'Networks' }).click()
    await page.getByLabel('Added by administrators').fill('')
    await page.getByRole('button', { name: 'Save networks' }).click()
    await expect(page.getByRole('status')).toBeVisible()
    await guest.dispose()
  })

  test('compare and sync', async ({ shell, page, quickId, request }) => {
    void shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const workspace = workspaces[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const stamp = Date.now()
    const created = await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name: `Backup ${stamp}`, protocol: 'ftp', host: 'ftp', port: 21, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    const { id: siteId } = (await created.json()) as { id: string }
    const leftFiles = `/api/v1/workspaces/${workspace}/sites/${quickId}/files`
    const rightFiles = `/api/v1/workspaces/${workspace}/sites/${siteId}/files`
    const folder = `shots-sync-${stamp}`
    for (const files of [leftFiles, rightFiles]) {
      await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })
    }
    const put = async (files: string, path: string, content: string) =>
      request.put(`${files}/content`, { params: { path, mode: 'overwrite' }, data: Buffer.from(content), headers: { ...headers, 'Content-Type': 'application/octet-stream' } })
    await put(leftFiles, `/upload/${folder}/report.csv`, 'name,total\n')
    await put(rightFiles, `/upload/${folder}/archive-2025.csv`, 'old data\n')
    await put(leftFiles, `/upload/${folder}/notes.txt`, 'the newer version, with more to say\n')
    await put(rightFiles, `/upload/${folder}/notes.txt`, 'short\n')

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${folder}`)
    await page.getByRole('button', { name: 'Split view' }).click()
    const second = page.getByRole('group', { name: 'Second pane' })
    await second.getByRole('combobox', { name: 'Site of the second pane' }).selectOption({ label: `Backup ${stamp}` })
    await second.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ }).dblclick()
    await second.getByRole('grid', { name: 'Files' }).getByRole('row', { name: new RegExp(folder) }).dblclick()
    await expect(second.getByRole('button', { name: folder, exact: true })).toHaveAttribute('aria-current', 'page')

    await second.getByRole('button', { name: 'Compare and sync with the other pane' }).click()
    const dialog = page.getByRole('dialog', { name: 'Compare and sync' })
    await expect(dialog.getByRole('list', { name: 'Comparison' })).toBeVisible()
    await shoot(page, '33-compare-plan')

    await dialog.getByRole('button', { name: /^Sync \d+ files?$/ }).click()
    await expect(dialog.getByText('Done').first()).toBeVisible()
    await shoot(page, '34-compare-done')
    await dialog.getByRole('button', { name: 'Close' }).click()

    for (const files of [leftFiles, rightFiles]) {
      await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
    }
    await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}`, { headers })
  })

  test('search', async ({ shell, page, quickId, request }) => {
    void shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = Date.now()
    const folder = `shots-search-${stamp}`
    const sub = `${folder}/quarter-2`
    await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })
    await request.post(`${files}/directories`, { headers, data: { path: `/upload/${sub}` } })
    await request.put(`${files}/content`, {
      params: { path: `/upload/${sub}/sales-report.csv`, mode: 'overwrite' },
      data: Buffer.from('name,total\n'),
      headers: { ...headers, 'Content-Type': 'application/octet-stream' },
    })

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${folder}`)
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Search' })
    await dialog.getByLabel('Search query').fill('report')
    await dialog.getByRole('button', { name: 'Search' }).click()
    await expect(dialog.getByRole('list', { name: 'Search results' })).toBeVisible()
    await shoot(page, '35-search-results')

    await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
  })

  test('site speed limit', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const stamp = Date.now()
    const name = `Backup ${stamp}`
    const created = await request.post(`/api/v1/workspaces/${workspaces[0].id}/sites`, {
      headers,
      data: { name, protocol: 'ftp', host: 'ftp', port: 21, username: 'sentinel', authMethod: 'password', password: 'sentinel', speedLimitBps: 4 * 1024 * 1024 },
    })
    expect(created.ok()).toBe(true)
    const siteId = ((await created.json()) as { id: string }).id

    await page.goto('/')
    const row = page.getByRole('treeitem', { name: new RegExp(name) })
    await row.hover()
    await page.getByRole('button', { name: `Actions for ${name}` }).click()
    await page.getByRole('menuitem', { name: 'Edit' }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit site' })
    await dialog.getByRole('tab', { name: 'Transfer' }).click()
    await expect(dialog.getByLabel('Speed limit (MiB/s)')).toHaveValue('4')
    await shoot(page, '36-site-speed-limit')

    await request.delete(`/api/v1/workspaces/${workspaces[0].id}/sites/${siteId}`, { headers })
  })

  test('proxy and jump host', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const stamp = Date.now()
    const name = `Via bastion ${stamp}`
    const created = await request.post(`/api/v1/workspaces/${workspaces[0].id}/sites`, {
      headers,
      data: {
        name, protocol: 'sftp', host: 'sftp', username: 'sentinel', password: 'sentinel',
        proxy: { kind: 'socks5', host: '9.9.9.9', port: 1080, username: 'proxyuser' }, proxyPassword: 'proxypass',
        jump: { host: '1.1.1.1', port: 22, username: 'bastion', authMethod: 'password' }, jumpPassword: 'bastionpass',
      },
    })
    expect(created.ok()).toBe(true)
    const siteId = ((await created.json()) as { id: string }).id

    await page.goto('/')
    const row = page.getByRole('treeitem', { name: new RegExp(name) })
    await row.hover()
    await page.getByRole('button', { name: `Actions for ${name}` }).click()
    await page.getByRole('menuitem', { name: 'Edit' }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit site' })
    await dialog.getByRole('tab', { name: 'Proxy and jump host' }).click()
    await expect(dialog.getByLabel('Jump host', { exact: true })).toHaveValue('1.1.1.1')
    await shoot(page, '37-proxy-and-jump')

    await request.delete(`/api/v1/workspaces/${workspaces[0].id}/sites/${siteId}`, { headers })
  })

  test('remote editor', async ({ shell, quickId, folder }) => {
    const { page } = shell
    const name = `shot-${Date.now()}.txt`
    await page.goto(`/sites/${quickId}?path=${encodeURIComponent(folder)}`)
    await expect(page.getByText('This folder is empty.')).toBeVisible()
    const grid = page.getByRole('grid', { name: 'Files' })
    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('server {\n  listen 443;\n}\n') })
    const row = grid.getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()
    await row.click()
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Edit' }).click()
    const dialog = page.getByRole('dialog', { name: new RegExp(name) })
    await expect(dialog.getByRole('textbox')).toContainText('listen 443')
    await shoot(page, '38-remote-editor')

    await dialog.getByRole('button', { name: 'Close' }).click()
    await grid.press('Delete')
    await page.getByRole('dialog', { name: 'Delete this file?' }).getByRole('button', { name: 'Delete' }).click()
  })
  test('site dialog with tabs', async ({ shell }) => {
    const { page } = shell
    await page.goto('/')
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await expect(dialog.getByRole('tab', { name: 'General' })).toHaveAttribute('aria-selected', 'true')
    await dialog.getByLabel('Name', { exact: true }).fill('7a')
    await dialog.getByLabel('Host', { exact: true }).fill('23.20.130.239')
    await dialog.getByLabel('Username', { exact: true }).fill('sftp7a')
    await shoot(page, '39-site-dialog-general')
    await dialog.getByRole('tab', { name: 'Advanced' }).click()
    await shoot(page, '40-site-dialog-advanced')
    await dialog.getByRole('tab', { name: 'General' }).click()
    await dialog.getByLabel('Protocol').selectOption('ftp')
    await dialog.getByLabel('Logon type').selectOption('anonymous')
    await shoot(page, '46-site-dialog-ftp-anonymous')
    await dialog.getByRole('tab', { name: 'Proxy and jump host' }).click()
    await shoot(page, '47-site-dialog-proxy')
    await dialog.getByRole('tab', { name: 'General' }).click()
    await dialog.getByLabel('Protocol').selectOption('sftp')
    await dialog.getByLabel('Name', { exact: true }).fill(`Asks ${Date.now()}`)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await dialog.getByLabel('Username', { exact: true }).fill('sentinel')
    await dialog.getByLabel('Logon type').selectOption('ask')
    await shoot(page, '48-site-dialog-ask')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await page.getByRole('treeitem', { name: /Asks/ }).first().click()
    await expect(page.getByRole('dialog', { name: 'Enter the password' })).toBeVisible()
    await shoot(page, '49-asks-for-password')
  })

  test('key file', async ({ shell }) => {
    const { page } = shell
    await page.goto('/')
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill('Deploy')
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await dialog.getByLabel('Username', { exact: true }).fill('sentinel')
    await dialog.getByLabel('Logon type').selectOption('key')
    await shoot(page, '50-key-file-empty')
    await dialog.getByLabel('Key file', { exact: true }).setInputFiles('e2e/keys/sentinel.pub')
    await expect(dialog.getByRole('alert')).toBeVisible()
    await shoot(page, '51-key-file-public')
    await dialog.getByRole('button', { name: 'Remove the key file' }).click()
    await dialog.getByLabel('Key file', { exact: true }).setInputFiles('e2e/keys/sentinel.ppk')
    await expect(dialog.getByText('Enter the passphrase of this key.')).toBeVisible()
    await shoot(page, '52-key-file-passphrase')
    await dialog.getByLabel('Key passphrase').fill('correct horse')
    await expect(dialog.getByText('Ready to use.')).toBeVisible()
    await shoot(page, '53-key-file-ready')
  })

  test('connection log', async ({ shell }) => {
    const { page } = shell
    await page.goto('/')
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(`Log ${Date.now()}`)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await dialog.getByLabel('Username', { exact: true }).fill('sentinel')
    await dialog.getByLabel('Password', { exact: true }).fill('sentinel')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await page.getByRole('treeitem', { name: /^Log / }).first().click()
    await expect(page.getByRole('list', { name: 'Connection log' }).getByText(/^Listed \//)).toBeVisible()
    await shoot(page, '54-connection-log')
    await page.getByRole('toolbar', { name: 'Toolbar' }).getByRole('button', { name: 'Disconnect from the server' }).click()
    await expect(page).toHaveURL(/\/$/)
    await page.goBack()
    await expect(page.getByText(/^Disconnected from /)).toBeVisible()
    await page.waitForTimeout(400)
    await shoot(page, '55-disconnected')
  })

  test('resizable panels', async ({ shell, quickId }) => {
    const { page } = shell
    await page.goto(`/sites/${quickId}?path=%2F`)
    await page.getByRole('grid', { name: 'Files' }).waitFor()
    await shoot(page, '56-panels')
    await page.keyboard.press('Control+b')
    await expect(page.getByRole('navigation', { name: 'Sites, panel hidden' })).toBeVisible()
    await page.waitForTimeout(300)
    await shoot(page, '57-sites-rail')
    await page.getByRole('button', { name: 'Browse the sites' }).click()
    await expect(page.getByRole('dialog', { name: 'Sites' })).toBeVisible()
    await page.waitForTimeout(400)
    await shoot(page, '58-sites-sheet')
    await page.keyboard.press('Escape')
    await page.keyboard.press('Control+b')
  })

  test('connection tabs', async ({ shell }) => {
    const { page } = shell
    await page.goto('/')
    for (const name of ['Production', 'Backup']) {
      await page.getByRole('button', { name: 'New site' }).click()
      const dialog = page.getByRole('dialog', { name: 'New site' })
      await dialog.getByLabel('Name', { exact: true }).fill(`${name} ${Date.now()}`)
      await dialog.getByLabel('Host', { exact: true }).fill('sftp')
      await dialog.getByLabel('Username', { exact: true }).fill('sentinel')
      await dialog.getByLabel('Password', { exact: true }).fill('sentinel')
      await dialog.getByRole('button', { name: 'Create site' }).click()
      await expect(dialog).toBeHidden()
    }
    await page.getByRole('treeitem', { name: /^Production/ }).first().click()
    await page.getByRole('treeitem', { name: /^Backup/ }).first().click({ modifiers: ['ControlOrMeta'] })
    await page.getByRole('grid', { name: 'Files' }).waitFor()
    await expect(page.getByRole('navigation', { name: 'Connection tabs' })).toBeVisible()
    await page.waitForTimeout(500)
    await shoot(page, '59-connection-tabs')
  })

  test('long folder keeps the transfers panel in view', async ({ shell, quickId, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const folder = `/upload/long-${Date.now()}`
    await request.post(`${files}/directories`, { headers: { 'X-CSRF-Token': me.csrfToken }, data: { path: folder } })
    for (let index = 0; index < 60; index++) {
      await request.put(`${files}/content?path=${encodeURIComponent(`${folder}/file-${String(index).padStart(2, '0')}.txt`)}`, {
        headers: { 'X-CSRF-Token': me.csrfToken, 'Content-Type': 'application/octet-stream' },
        data: Buffer.from(`file ${index}\n`),
      })
    }
    await page.goto(`/sites/${quickId}?path=${encodeURIComponent(folder)}`)
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /file-00/ })).toBeVisible()
    const dock = page.getByRole('region', { name: 'Transfers' })
    await expect(dock).toBeInViewport({ ratio: 1 })
    await shoot(page, '41-long-folder-dock')
    await request.delete(`${files}?path=${encodeURIComponent(folder)}&recursive=true`, { headers: { 'X-CSRF-Token': me.csrfToken } })
  })
  test('loading, failure and preparing states', async ({ shell, quickId }) => {
    const { page } = shell
    let release: () => void = () => undefined
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/api/v1/admin/limits', async (route) => {
      await held
      await route.continue()
    })
    await page.route('**/api/v1/admin/networks', (route) =>
      route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'x', status: 503, code: 'network_error' }) }),
    )
    await page.goto('/')
    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Administration' }).click()
    await page.getByRole('tab', { name: 'Limits' }).click()
    await expect(page.getByRole('status')).toHaveText('Loading limits…')
    await shoot(page, '42-loading-spinner')
    release()
    await page.getByRole('tab', { name: 'Networks' }).click()
    await expect(page.getByRole('alert')).toContainText('Could not reach the server.')
    await shoot(page, '43-load-failed-retry')
    await page.unroute('**/api/v1/admin/networks')
    await page.getByRole('button', { name: 'Retry' }).click()
    await expect(page.getByLabel('Added by administrators')).toBeVisible()

    await page.goto(`/sites/${quickId}?path=%2F`)
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /upload/ }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Download as ZIP' }).click()
    await expect(page.getByRole('list', { name: 'Downloads being prepared' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Transfers' }).getByRole('listitem').filter({ hasText: 'upload.zip' })).toBeVisible()
    await shoot(page, '44-zip-download-feedback')

    await page.getByRole('button', { name: 'Account' }).click()
    await page.getByRole('menuitem', { name: 'Settings' }).click()
    await page.getByLabel('Double click on a file').scrollIntoViewIfNeeded()
    await shoot(page, '45-settings-double-click')
  })
})
