import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

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

/**
 * Takes the screenshots of the team screens and of the vault notice, to review the interface without opening it.
 * It is skipped unless `SENTINEL_E2E_SCREENSHOTS` is set, so the normal run does not write files.
 *
 * Scenarios:
 * - the team switcher, the new team dialog, the members page, the invite dialog in both steps, the invitations tab, the invitation page
 *   for a new account and for the wrong account, the members page of a viewer, the row menu and the removal dialog
 * - the locked vault in a new site and a saved site, the unlock dialog, and a quick connection that works while locked
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
  })
})
