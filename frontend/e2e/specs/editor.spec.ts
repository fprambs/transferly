import { readFile } from 'node:fs/promises'

import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Opens a folder of the quick connection that the global setup made to the SFTP server of the test stack; the fresh folders of the tests are empty. */
async function openFolder(page: Page, quickId: string, folder: string): Promise<void> {
  await page.goto(`/sites/${quickId}?path=${encodeURIComponent(folder)}`)
  await expect(page.getByText('This folder is empty.')).toBeVisible()
}

/**
 * The remote editor against a real SFTP server.
 *
 * Scenarios:
 * - a file opens with its content, is edited and saved, and the server keeps the new content
 * - a file changed on the server since it was opened offers to reload the other version or overwrite it anyway
 */
test.describe('remote editor', () => {
  test('edits and saves a file on the server', async ({ shell, quickId, folder }) => {
    const { page } = shell
    const name = `e2e-edit-${Date.now()}-${Math.floor(Math.random() * 1e6)}.txt`
    await openFolder(page, quickId, folder)
    const grid = page.getByRole('grid', { name: 'Files' })

    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('first line\n') })
    const row = grid.getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()

    await row.dblclick()
    const dialog = page.getByRole('dialog', { name: new RegExp(name) })
    await expect(dialog.getByRole('textbox')).toContainText('first line')

    await dialog.getByRole('textbox').click()
    await page.keyboard.press('Control+End')
    await page.keyboard.type('second line')
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(dialog.getByRole('button', { name: 'Save' })).toBeEnabled()
    await dialog.getByRole('button', { name: 'Close' }).click()
    await expect(dialog).toBeHidden()

    const download = page.waitForEvent('download')
    await row.click()
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()
    const downloaded = await download
    const path = await downloaded.path()
    expect(await readFile(path, 'utf8')).toBe('first line\nsecond line')

    await grid.press('Delete')
    await page.getByRole('dialog', { name: 'Delete this file?' }).getByRole('button', { name: 'Delete' }).click()
    await expect(row).toBeHidden()
  })

  test('offers to reload or overwrite a file that changed on the server while it was open', async ({ shell, quickId, folder }) => {
    const { page } = shell
    const name = `e2e-conflict-${Date.now()}-${Math.floor(Math.random() * 1e6)}.txt`
    await openFolder(page, quickId, folder)
    const grid = page.getByRole('grid', { name: 'Files' })

    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('mine\n') })
    const row = grid.getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()

    await row.dblclick()
    const dialog = page.getByRole('dialog', { name: new RegExp(name) })
    await expect(dialog.getByRole('textbox')).toContainText('mine')

    const other = await page.context().newPage()
    await other.goto(page.url())
    await expect(other.getByRole('grid', { name: 'Files' })).toBeVisible()
    await other.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('someone else wrote this\n') })
    await other.getByRole('dialog').getByRole('button', { name: 'Overwrite' }).click()
    await expect(other.getByRole('dialog')).toBeHidden()
    await other.close()

    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(dialog.getByText('This file changed since you opened it')).toBeVisible()

    await dialog.getByRole('button', { name: 'Overwrite anyway' }).click()
    await expect(dialog.getByText('This file changed since you opened it')).toBeHidden()
    await dialog.getByRole('button', { name: 'Close' }).click()

    const download = page.waitForEvent('download')
    await row.click()
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()
    const downloaded = await download
    const path = await downloaded.path()
    expect(await readFile(path, 'utf8')).toBe('mine\n')

    await grid.press('Delete')
    await page.getByRole('dialog', { name: 'Delete this file?' }).getByRole('button', { name: 'Delete' }).click()
    await expect(row).toBeHidden()
  })
})
