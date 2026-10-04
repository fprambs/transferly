import type { Locator, Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/** Offers the WebDAV connector from the store in Administration, as an administrator does before anyone can use it. */
async function offerWebDAV(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Account' }).click()
  await page.getByRole('menuitem', { name: 'Settings' }).click()
  await page.getByRole('navigation', { name: 'Settings' }).getByRole('button', { name: 'Administration', exact: true }).click()
  await page.getByRole('tab', { name: 'Connectors' }).click()
  const card = page.getByRole('listitem', { name: 'WebDAV' })
  await expect(card).toContainText('Beta')
  const offer = card.getByRole('switch', { name: 'Offer WebDAV' })
  if (!(await offer.isChecked())) {
    await offer.click()
  }
  await expect(card).toContainText('Offered')
  await page.goto('/')
}

/** Starts a WebDAV site on the Apache server of the test stack, over plain HTTP, on the path given, and returns its dialog. */
async function startSite(page: Page, name: string, path: string): Promise<Locator> {
  await page.getByRole('button', { name: 'New site' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'New site' })
  await dialog.getByRole('button', { name: /^WebDAV/ }).click()
  await dialog.getByLabel('Name', { exact: true }).fill(name)
  await dialog.getByLabel('Server').fill('webdav')
  await dialog.getByRole('checkbox', { name: 'Use HTTPS' }).click()
  await expect(dialog.getByRole('note')).toContainText('travel in clear text')
  await expect(dialog.getByLabel('Port', { exact: true })).toHaveAttribute('placeholder', '80')
  await dialog.getByLabel('Path').fill(path)
  return dialog
}

/**
 * The WebDAV connector (ADR-0026) against Apache httpd with mod_dav in the test stack (webdav:80, plain HTTP).
 *
 * Scenarios:
 * - an administrator offers WebDAV; a site is created with its server, path, username and password, and the form warns while
 *   HTTPS is off
 * - the site opens its folder: a folder is created and entered, a file is uploaded into it and listed, renamed and deleted
 * - a public share opens anonymously and lists its file
 */
test.describe('webdav', () => {
  test('creates WebDAV sites and works with their files', async ({ shell }) => {
    test.setTimeout(90_000)
    const { page } = shell
    const stamp = Date.now()
    const folder = `e2e-${stamp}`
    const file = `notes-${stamp}.txt`
    await offerWebDAV(page)

    let dialog = await startSite(page, `Dav ${stamp}`, '/webdav/')
    await dialog.getByLabel('Username', { exact: true }).fill('transferly')
    await dialog.getByLabel('Password', { exact: true }).fill('transferly')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    await page.getByRole('treeitem', { name: new RegExp(`Dav ${stamp}`) }).dblclick()
    const grid = page.getByRole('grid', { name: 'Files' })
    await expect(page.getByText('This folder is empty.').or(grid)).toBeVisible()
    await page.getByRole('button', { name: 'New folder' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(folder)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    await grid.getByRole('row', { name: new RegExp(folder) }).dblclick()
    await expect(page).toHaveURL(new RegExp(`path=%2F${folder}$`))

    await page.getByLabel('Choose files to upload').setInputFiles({ name: file, mimeType: 'text/plain', buffer: Buffer.from('kept on webdav\n') })
    const row = grid.getByRole('row', { name: new RegExp(file) })
    await expect(row).toBeVisible()
    await expect(row).toContainText('15 B')

    await row.click()
    await grid.press('F2')
    await page.getByRole('dialog').getByLabel('New name').fill(`renamed-${file}`)
    await page.getByRole('dialog').getByRole('button', { name: 'Rename' }).click()
    const renamed = grid.getByRole('row', { name: new RegExp(`renamed-${file}`) })
    await expect(renamed).toBeVisible()

    await renamed.click()
    await grid.press('Delete')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click()
    await expect(renamed).toBeHidden()

    dialog = await startSite(page, `Public ${stamp}`, '/public/')
    await choose(dialog.getByLabel('Logon type'), 'Anonymous')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()
    await page.getByRole('treeitem', { name: new RegExp(`Public ${stamp}`) }).dblclick()
    await expect(grid.getByRole('row', { name: /readme\.txt/ })).toBeVisible()
  })
})
