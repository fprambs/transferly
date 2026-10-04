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
 * Which sites are connected (4.11), against the SFTP server of the isolated stack.
 *
 * Scenarios:
 * - opening a site marks it connected in the sidebar
 * - opening another site from the sidebar closes the connections of the first, whose mark goes away, and marks the second
 * - disconnecting from the menu of a connected site removes its mark
 */
test.describe('connection state', () => {
  test('marks the connected site and moves the mark when another is opened', async ({ shell }) => {
    const { page } = shell
    const stamp = Date.now()
    const first = `First ${stamp}`
    const second = `Second ${stamp}`
    await saveSite(page, first)
    await saveSite(page, second)
    const firstRow = page.getByRole('treeitem', { name: new RegExp(first) })
    const secondRow = page.getByRole('treeitem', { name: new RegExp(second) })

    await firstRow.dblclick()
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()
    await expect(firstRow.getByText('Connected')).toHaveCount(1)

    await secondRow.dblclick()
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()
    await expect(secondRow.getByText('Connected')).toHaveCount(1)
    await expect(firstRow.getByText('Connected')).toHaveCount(0)

    await secondRow.getByRole('button', { name: `Actions for ${second}` }).click()
    await page.getByRole('menuitem', { name: 'Disconnect' }).click()
    await expect(secondRow.getByText('Connected')).toHaveCount(0)
    await expect(page).toHaveURL(/\/$/)
  })
})
