import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Saves an SFTP site to the test server with the given name. */
async function saveSite(page: Page, name: string): Promise<void> {
  await page.getByRole('button', { name: 'New site' }).click()
  const dialog = page.getByRole('dialog', { name: 'New site' })
  await dialog.getByRole('button', { name: /^SFTP/ }).click()
  await dialog.getByLabel('Name', { exact: true }).fill(name)
  await dialog.getByLabel('Host', { exact: true }).fill('sftp')
  await dialog.getByLabel('Username', { exact: true }).fill('transferly')
  await dialog.getByLabel('Password', { exact: true }).fill('transferly')
  await dialog.getByRole('button', { name: 'Create site' }).click()
  await expect(dialog).toBeHidden()
}

/**
 * Connection tabs (4.12) against the SFTP server of the isolated stack.
 *
 * Scenarios:
 * - Ctrl with a double click opens a site in a new tab next to the one open, both stay connected, and clicking a tab goes back to its site and folder
 * - the tabs and the active one come back after a reload
 * - closing the active tab shows the other one and disconnects its site, whose green dot goes out
 * - the tab of a site that does not exist closes for good, even when the browser kept tabs of a workspace that no longer exists, as after
 *   a database was reset; those are forgotten
 */
test.describe('connection tabs', () => {
  test('opens, switches, remembers and closes tabs', async ({ shell }) => {
    const { page } = shell
    const stamp = Date.now()
    const first = `Tab one ${stamp}`
    const second = `Tab two ${stamp}`
    await saveSite(page, first)
    await saveSite(page, second)
    const grid = page.getByRole('grid', { name: 'Files' })

    await page.getByRole('treeitem', { name: new RegExp(first) }).dblclick()
    await grid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(page).toHaveURL(/path=%2Fupload/)
    await page.getByRole('treeitem', { name: new RegExp(second) }).dblclick({ modifiers: ['ControlOrMeta'] })
    await expect(grid.getByRole('row', { name: /upload/ })).toBeVisible()
    const tabs = page.getByRole('navigation', { name: 'Connection tabs' })
    await expect(tabs.getByRole('button', { name: new RegExp(`^(Connected\\s*)?${first}$`) })).toBeVisible()
    await expect(tabs.getByRole('button', { name: new RegExp(`^(Connected\\s*)?${second}$`) })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('treeitem', { name: new RegExp(first) }).getByText('Connected')).toHaveCount(1)

    await tabs.getByRole('button', { name: new RegExp(`^(Connected\\s*)?${first}$`) }).click()
    await expect(page).toHaveURL(/path=%2Fupload/)

    await page.reload()
    await expect(page).toHaveURL(/path=%2Fupload/)
    await expect(tabs.getByRole('button', { name: new RegExp(`^(Connected\\s*)?${first}$`) })).toHaveAttribute('aria-current', 'page')
    await expect(tabs.getByRole('button', { name: new RegExp(`^(Connected\\s*)?${second}$`) })).toBeVisible()

    await tabs.getByRole('button', { name: `Close ${first}` }).click()
    await expect(tabs.getByRole('button', { name: new RegExp(`^(Connected\\s*)?${second}$`) })).toHaveAttribute('aria-current', 'page')
    await expect(tabs.getByRole('button', { name: `Close ${first}` })).toHaveCount(0)
    await expect(page.getByRole('treeitem', { name: new RegExp(first) }).getByText('Connected')).toHaveCount(0)
    await expect(page.getByRole('treeitem', { name: new RegExp(second) }).getByText('Connected')).toHaveCount(1)
  })

  test('closes the tab of a missing site even with tabs of a workspace that no longer exists', async ({ shell }) => {
    const { page } = shell
    await page.evaluate(() => {
      const stored = JSON.parse(localStorage.getItem('transferly-tabs') ?? '{"state":{"tabs":[],"activeId":null},"version":0}')
      stored.state.tabs.push({ id: 'tab-stale', workspaceId: '00000000-0000-4000-8000-000000000001', siteId: '00000000-0000-4000-8000-000000000002', path: '/' })
      localStorage.setItem('transferly-tabs', JSON.stringify(stored))
    })
    await page.goto('/sites/00000000-0000-4000-8000-000000000003?path=%2F')
    const close = page.getByRole('button', { name: 'Close Site not found' })
    await expect(close).toBeVisible()
    await close.click()
    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('button', { name: /^Close / })).toHaveCount(0)
    const kept = await page.evaluate(() => localStorage.getItem('transferly-tabs') ?? '')
    expect(kept).not.toContain('tab-stale')
  })
})
