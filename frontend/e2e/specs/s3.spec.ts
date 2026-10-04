import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Offers the S3 connector from the store in Administration, as an administrator does before anyone can use it. */
async function offerS3(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Account' }).click()
  await page.getByRole('menuitem', { name: 'Settings' }).click()
  await page.getByRole('navigation', { name: 'Settings' }).getByRole('button', { name: 'Administration', exact: true }).click()
  await page.getByRole('tab', { name: 'Connectors' }).click()
  const card = page.getByRole('listitem', { name: 'S3' })
  await expect(card).toContainText('Beta')
  const offer = card.getByRole('switch', { name: 'Offer S3' })
  if (!(await offer.isChecked())) {
    await offer.click()
  }
  await expect(card).toContainText('Offered')
  await page.goto('/')
}

/**
 * The S3 connector (ADR-0023) against a real S3 server of the test stack (Versity Gateway on s3:7070, plain HTTP).
 *
 * Scenarios:
 * - an administrator offers S3 from the store; a site is then created from its schema, with its endpoint, bucket and access key, and the
 *   form warns while HTTPS is off
 * - the site opens its bucket: a folder is created and entered, a file is uploaded into it and listed, renamed and deleted
 */
test.describe('s3', () => {
  test('creates an S3 site and works with the files of its bucket', async ({ shell }) => {
    test.setTimeout(90_000)
    const { page } = shell
    const stamp = Date.now()
    const name = `Bucket ${stamp}`
    const folder = `e2e-${stamp}`
    const file = `notes-${stamp}.txt`
    await offerS3(page)

    await page.getByRole('button', { name: 'New site' }).first().click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByRole('button', { name: /^S3/ }).click()
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await expect(dialog.getByLabel('Endpoint')).toHaveAttribute('placeholder', 's3.amazonaws.com')
    await dialog.getByLabel('Endpoint').fill('s3')
    await dialog.getByRole('checkbox', { name: 'Use HTTPS' }).click()
    await expect(dialog.getByRole('note')).toContainText('travel in clear text')
    await expect(dialog.getByLabel('Port', { exact: true })).toHaveAttribute('placeholder', '80')
    await dialog.getByLabel('Port', { exact: true }).fill('7070')
    await dialog.getByLabel('Bucket').fill('transferly')
    await dialog.getByLabel('Access key ID').fill('transferly')
    await dialog.getByLabel('Secret access key').fill('transferly-secret')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const grid = page.getByRole('grid', { name: 'Files' })
    await expect(page.getByText('This folder is empty.').or(grid)).toBeVisible()
    await page.getByRole('button', { name: 'New folder' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(folder)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    await grid.getByRole('row', { name: new RegExp(folder) }).dblclick()
    await expect(page).toHaveURL(new RegExp(`path=%2F${folder}$`))

    await page.getByLabel('Choose files to upload').setInputFiles({ name: file, mimeType: 'text/plain', buffer: Buffer.from('kept in a bucket\n') })
    const row = grid.getByRole('row', { name: new RegExp(file) })
    await expect(row).toBeVisible()
    await expect(row).toContainText('17 B')

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
