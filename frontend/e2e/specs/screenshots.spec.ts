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
 * - the locked vault in a new site and a saved site, the unlock dialog, and a quick connection that works while locked
 * - comparing a saved FTP site against the SFTP quick connection: the differences and the plan running
 * - searching a folder recursively: the results found below a subfolder
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
    await expect(page.getByText('Your vault is locked')).toBeVisible()
    await shoot(page, '15-saved-site-vault-locked')

    await page.goto(`/sites/${quickId}?path=%2F`)
    await expect(page.getByRole('row').first()).toBeVisible()
    await shoot(page, '16-quick-connection-vault-locked')

    await page.goto(`/sites/${siteId}?path=%2F`)
    await page.getByRole('button', { name: 'Unlock the vault' }).click()
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
})
