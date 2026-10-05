import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Saves an SFTP site with the test account on a host of the isolated stack, with no default remote folder, and opens it, trusting its
 * key when asked. */
async function openSite(page: Page, name: string, host: string): Promise<void> {
  await page.getByRole('button', { name: 'New site' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'New site' })
  await dialog.getByRole('button', { name: /^SFTP/ }).click()
  await dialog.getByLabel('Name', { exact: true }).fill(name)
  await dialog.getByLabel('Host', { exact: true }).fill(host)
  await dialog.getByLabel('Username', { exact: true }).fill('transferly')
  await dialog.getByLabel('Password', { exact: true }).fill('transferly')
  await dialog.getByRole('button', { name: 'Create site' }).click()
  await expect(dialog).toBeHidden()
  await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
  const trust = page.getByRole('button', { name: 'Trust and connect' })
  const grid = page.getByRole('grid', { name: 'Files' })
  await expect(trust.or(grid).or(page.getByText('This folder is empty.'))).toBeVisible()
  if (await trust.isVisible()) {
    await trust.click()
  }
}

/**
 * The SSH tools of SFTP sites (ADR-0027): an OpenSSH server with a shell (ssh:22) and one restricted to SFTP (sftp:22).
 *
 * Scenarios:
 * - a site without a default remote folder opens in the home folder of the account, where the server opens sessions, not at the root
 * - on the server with a shell, a folder with a file is compressed into a ZIP next to it, the ZIP is extracted into a new folder named
 *   after it, which holds the folder with the same file, and the size of the folder is measured
 * - on the server restricted to SFTP the explorer offers none of these
 */
test.describe('ssh tools', () => {
  test('compresses, extracts and measures on a server that runs commands', async ({ shell }) => {
    test.setTimeout(90_000)
    const { page } = shell
    const stamp = Date.now()
    const folder = `proj-${stamp}`
    await openSite(page, `Shell ${stamp}`, 'ssh')
    await expect(page).toHaveURL(/path=%2Fhome%2Ftransferly$/)
    const grid = page.getByRole('grid', { name: 'Files' })

    await page.getByRole('button', { name: 'New folder' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(folder)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    await grid.getByRole('row', { name: new RegExp(folder) }).dblclick()
    await page.getByLabel('Choose files to upload').setInputFiles({ name: 'readme.txt', mimeType: 'text/plain', buffer: Buffer.from('compressed on the server\n') })
    await expect(grid.getByRole('row', { name: /readme\.txt/ })).toBeVisible()
    await page.getByRole('button', { name: 'Up one level' }).click()

    await grid.getByRole('row', { name: new RegExp(`${folder}\\b`) }).first().click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Compress…' }).click()
    const dialog = page.getByRole('dialog', { name: `Compress ${folder}` })
    await expect(dialog.getByLabel('Archive name')).toHaveValue(`${folder}.zip`)
    await dialog.getByRole('button', { name: 'Compress' }).click()
    await expect(dialog).toBeHidden()
    const archive = grid.getByRole('row', { name: new RegExp(`${folder}\\.zip`) })
    await expect(archive).toBeVisible()

    await archive.click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Extract here' }).click()
    const extracted = grid.getByRole('row', { name: new RegExp(`${folder} \\(1\\)`) })
    await expect(extracted).toBeVisible()
    await extracted.dblclick()
    await expect(page).toHaveURL(/%281%29$/)
    await expect(grid.getByRole('row', { name: new RegExp(folder) })).toHaveCount(1)
    await grid.getByRole('row', { name: new RegExp(folder) }).dblclick()
    await expect(grid.getByRole('row', { name: /readme\.txt/ })).toBeVisible()
    await page.getByRole('button', { name: 'Up one level' }).click()
    await page.getByRole('button', { name: 'Up one level' }).click()

    await grid.getByRole('row', { name: new RegExp(`${folder}\\b`) }).first().click()
    const details = page.getByRole('complementary', { name: 'Details' })
    await details.getByRole('button', { name: 'Measure' }).click()
    await expect(details).toContainText(/\d+(\.\d+)? KB/)
  })

  test('offers none of them on a server restricted to SFTP', async ({ shell }) => {
    const { page } = shell
    await openSite(page, `Restricted ${Date.now()}`, 'sftp')
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /upload/ }).click({ button: 'right' })
    await expect(page.getByRole('menuitem', { name: 'Rename' })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: 'Compress…' })).toHaveCount(0)
  })
})
