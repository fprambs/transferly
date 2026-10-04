import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Offers a connector from the store in Administration, as an administrator does before anyone can use it. */
async function offer(page: Page, name: string): Promise<void> {
  await page.getByRole('button', { name: 'Account' }).click()
  await page.getByRole('menuitem', { name: 'Settings' }).click()
  await page.getByRole('navigation', { name: 'Settings' }).getByRole('button', { name: 'Administration', exact: true }).click()
  await page.getByRole('tab', { name: 'Connectors' }).click()
  const card = page.getByRole('listitem', { name })
  const toggle = card.getByRole('switch', { name: `Offer ${name}` })
  if (!(await toggle.isChecked())) {
    await toggle.click()
  }
  await expect(card).toContainText('Offered')
  await page.goto('/')
}

/**
 * The Google Drive connector (ADR-0024) against the fake of Google of the test stack, whose consent page agrees at once.
 *
 * Scenarios:
 * - an administrator offers Google Drive; a site is created from the catalog, connecting the Google account in a popup, which comes back
 *   with the account the form shows
 * - the site opens My Drive: its Google document is listed with the extension of its export and downloads exported; a folder is created
 *   and entered, a file is uploaded into it, renamed and moved to the trash
 */
test.describe('google drive', () => {
  test('connects a Google account and works with the files of its drive', async ({ shell }) => {
    test.setTimeout(90_000)
    const { page } = shell
    const stamp = Date.now()
    const name = `Drive ${stamp}`
    const folder = `e2e-${stamp}`
    const file = `notes-${stamp}.txt`
    await offer(page, 'Google Drive')

    await page.getByRole('button', { name: 'New site' }).first().click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByRole('button', { name: /^Google Drive/ }).click()
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog.getByText('Connect the account first.')).toBeVisible()

    const popup = page.waitForEvent('popup')
    await dialog.getByRole('button', { name: 'Connect with Google' }).click()
    await (await popup).waitForEvent('close')
    await expect(dialog.getByText('tester@example.com')).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Use another account' })).toBeVisible()
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const grid = page.getByRole('grid', { name: 'Files' })
    const plan = grid.getByRole('row', { name: /Plan\.docx/ })
    await expect(plan).toBeVisible()
    await expect(grid.getByRole('row', { name: /Shared notes/ })).toBeVisible()
    await plan.click()
    const download = page.waitForEvent('download')
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()
    const saved = await (await download).createReadStream()
    const chunks: Buffer[] = []
    for await (const chunk of saved) {
      chunks.push(chunk as Buffer)
    }
    expect(Buffer.concat(chunks).toString()).toContain('The plan of the quarter.')

    await page.getByRole('button', { name: 'New folder' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(folder)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    await grid.getByRole('row', { name: new RegExp(folder) }).dblclick()
    await expect(page).toHaveURL(new RegExp(`path=%2F${folder}$`))

    await page.getByLabel('Choose files to upload').setInputFiles({ name: file, mimeType: 'text/plain', buffer: Buffer.from('kept in a drive\n') })
    const row = grid.getByRole('row', { name: new RegExp(file) })
    await expect(row).toBeVisible()
    await expect(row).toContainText('16 B')

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
  })
})
