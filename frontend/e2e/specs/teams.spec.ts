import { AxeBuilder } from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'
import { settle } from '../settle.ts'

/** Closes the settings dialog when it is open, so the top bar can be used again. */
async function closeSettings(page: Page): Promise<void> {
  const settings = page.getByRole('dialog', { name: 'Settings' })
  if (await settings.isVisible()) {
    await settings.getByRole('button', { name: 'Close settings' }).click()
    await expect(settings).toHaveCount(0)
  }
}

/** Moves the settings dialog to one of the sections of its side bar. */
async function openSection(page: Page, name: string): Promise<void> {
  await page.getByRole('navigation', { name: 'Settings' }).getByRole('button', { name, exact: true }).click()
}

/** The rows of a table of the settings dialog, without its header. */
function rowsOf(page: Page, table: string) {
  return page.getByRole('table', { name: table }).locator('tbody tr')
}

/** Chooses the team in the workspace switcher of a page and opens its members in the settings dialog. */
async function openTeamMembers(page: Page, name: string): Promise<void> {
  await closeSettings(page)
  await page.getByRole('button', { name: 'Workspace' }).click()
  await page.getByRole('menuitem', { name: new RegExp(`^${name}`) }).click()
  await page.getByRole('button', { name: 'Workspace' }).click()
  await page.getByRole('menuitem', { name: 'Members', exact: true }).click()
  await expect(page).toHaveURL(/settings=members/)
}

/**
 * Teams against the production image: a team workspace, an invitation, a new account that joins through it, and removing them.
 *
 * Scenarios:
 * - the owner creates a team from the switcher, invites an address as a viewer and gets a one-time link
 * - somebody without an account opens the link in a clean browser, sees what it offers, creates their account and lands on the members
 *   section of the settings as a viewer whose key waits for their vault, and cannot invite or change anyone
 * - the owner sees the new member, removes them, and the link no longer works
 * - the audit log words the creation, the invitation, the join and the removal, and narrows by group
 * - a revoked invitation and a made up token show the same page, which does not say why
 * - the members page, with its dialogs, the audit log and the invitation page have no WCAG violations in the light and dark themes
 */
test.describe('teams', () => {
  test.describe.configure({ mode: 'serial' })

  const stamp = Date.now()
  const team = `e2e-team-${stamp}`
  const invitee = `e2e-invitee-${stamp}@example.com`
  let link = ''

  test('creates a team and an invitation', async ({ shell, page }) => {
    void shell
    await page.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitem', { name: 'New team workspace' }).click()
    const dialog = page.getByRole('dialog', { name: 'New team workspace' })
    await dialog.getByLabel('Name').fill(team)
    await dialog.getByRole('button', { name: 'Create' }).click()

    await expect(page).toHaveURL(/settings=members/)
    await expect(page.getByRole('heading', { name: 'Members', exact: true })).toBeVisible()
    await expect(rowsOf(page, 'Members')).toHaveCount(1)

    await page.getByRole('button', { name: 'Invite member' }).click()
    const invite = page.getByRole('dialog', { name: `Invite to ${team}` })
    await invite.getByLabel('Email').fill(invitee)
    await invite.getByRole('radio', { name: /Viewer/ }).check()
    await invite.getByRole('button', { name: 'Create invitation' }).click()

    const ready = page.getByRole('dialog', { name: 'Invitation ready' })
    link = await ready.getByLabel('One-time link').inputValue()
    expect(link).toMatch(/\/invite#[A-Za-z0-9_-]{43}$/)
    await ready.getByRole('button', { name: 'Done' }).click()
    await page.getByRole('tab', { name: /Invitations/ }).click()
    await expect(page.getByRole('table', { name: 'Invitations' })).toContainText(invitee)
  })

  test('lets the invited address create an account and join', async ({ browser, baseURL }) => {
    const context = await browser.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    const guest = await context.newPage()
    await guest.goto(link)

    await expect(guest.getByRole('heading', { name: `You were invited to ${team}` })).toBeVisible()
    await expect(guest.getByLabel('Email')).toHaveValue(invitee)
    await guest.getByLabel('Account password').fill('a long guest password')
    await guest.getByLabel('Repeat password').fill('a long guest password')
    await guest.getByRole('button', { name: 'Create account and join' }).click()

    await expect(guest).toHaveURL(/settings=members/)
    await expect(guest.getByRole('button', { name: 'Workspace', includeHidden: true })).toContainText(team)
    const list = rowsOf(guest, 'Members')
    await expect(list).toHaveCount(2)
    await expect(list.filter({ hasText: invitee })).toContainText('Waiting for vault')
    await expect(guest.getByRole('button', { name: 'Invite member' })).toHaveCount(0)
    await expect(guest.getByLabel(/^Role of /)).toHaveCount(0)

    await guest.goto(link)
    await expect(guest.getByRole('heading', { name: 'This invitation cannot be used' })).toBeVisible()
    await context.close()
  })

  test('lets the owner see the new member and remove them', async ({ shell, page }) => {
    void shell
    await openTeamMembers(page, team)
    const list = rowsOf(page, 'Members')
    await expect(list.filter({ hasText: invitee })).toContainText('Viewer')

    await page.getByRole('button', { name: `Actions for ${invitee}` }).click()
    await page.getByRole('menuitem', { name: 'Remove from team' }).click()
    const dialog = page.getByRole('dialog', { name: `Remove ${invitee}?` })
    await expect(dialog).toContainText('The workspace key will be rotated')
    await dialog.getByRole('button', { name: 'Remove and rotate key' }).click()

    await expect(list).toHaveCount(1)
  })

  test('records what happened in the audit log', async ({ shell, page }) => {
    void shell
    await openTeamMembers(page, team)
    await openSection(page, 'Audit log')

    const log = page.getByRole('table', { name: 'Audit log' })
    await expect(log).toContainText(`created the workspace ${team}`)
    await expect(log).toContainText(`invited ${invitee} as Viewer`)
    await expect(log).toContainText(`${invitee} joined as Viewer`)
    await expect(log).toContainText(`removed ${invitee}`)
    await expect(log.locator('tbody tr').first()).toContainText('e2e@example.com')

    await choose(page.getByLabel('Show'), 'Invitations')
    await expect(log).not.toContainText('created the workspace')
    await expect(log).toContainText(`invited ${invitee}`)
  })

  test('shows the same page for a revoked invitation and a made up token', async ({ shell, page, browser, baseURL }) => {
    void shell
    await openTeamMembers(page, team)
    await page.getByRole('button', { name: 'Invite member' }).click()
    const invite = page.getByRole('dialog', { name: `Invite to ${team}` })
    await invite.getByLabel('Email').fill(`e2e-revoked-${stamp}@example.com`)
    await invite.getByRole('button', { name: 'Create invitation' }).click()
    const ready = page.getByRole('dialog', { name: 'Invitation ready' })
    const revoked = await ready.getByLabel('One-time link').inputValue()
    await ready.getByRole('button', { name: 'Done' }).click()
    await page.getByRole('tab', { name: /Invitations/ }).click()
    await page.getByRole('button', { name: `Revoke the invitation of e2e-revoked-${stamp}@example.com` }).click()
    await expect(page.getByText('No invitations are waiting.')).toBeVisible()

    const context = await browser.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    const guest = await context.newPage()
    const messages: string[] = []
    for (const target of [revoked, `${baseURL}/invite#${'x'.repeat(43)}`]) {
      await guest.goto(target)
      await expect(guest.getByRole('alert')).toBeVisible()
      messages.push((await guest.getByRole('alert').textContent()) ?? '')
    }
    expect(messages[0]).toBe(messages[1])
    await context.close()
  })

  test('has no WCAG violations on the members and invitation pages', async ({ shell, page, browser, baseURL }) => {
    for (const theme of ['transferly-dark', 'transferly-light']) {
      await closeSettings(page)
      await shell.setTheme(theme)
      await openTeamMembers(page, team)
      await page.getByRole('button', { name: 'Invite member' }).click()
      await expect(page.getByRole('dialog', { name: `Invite to ${team}` })).toBeVisible()
      await settle(page)
      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
      expect(results.violations, theme).toEqual([])
      await page.keyboard.press('Escape')
      await settle(page)
      const members = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
      expect(members.violations, `${theme} members`).toEqual([])
      await openSection(page, 'Audit log')
      await expect(page.getByRole('table', { name: 'Audit log' })).toBeVisible()
      await settle(page)
      const trail = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
      expect(trail.violations, `${theme} audit`).toEqual([])
      await openTeamMembers(page, team)
    }

    await page.getByRole('button', { name: 'Invite member' }).click()
    const invite = page.getByRole('dialog', { name: `Invite to ${team}` })
    await invite.getByLabel('Email').fill(`e2e-axe-${stamp}@example.com`)
    await invite.getByRole('button', { name: 'Create invitation' }).click()
    const ready = page.getByRole('dialog', { name: 'Invitation ready' })
    const axeLink = await ready.getByLabel('One-time link').inputValue()
    await settle(page)
    expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations).toEqual([])

    const context = await browser.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    const guest = await context.newPage()
    await guest.goto(axeLink)
    await expect(guest.getByRole('heading', { name: `You were invited to ${team}` })).toBeVisible()
    await settle(guest)
    expect((await new AxeBuilder({ page: guest }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations).toEqual([])
    await context.close()
  })
})
