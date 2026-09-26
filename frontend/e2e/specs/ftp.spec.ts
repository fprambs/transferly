import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'

import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Fills the quick connect dialog for a server of the test stack and presses Connect. */
async function quickConnect(page: Page, protocol: 'SFTP' | 'FTPS' | 'FTP', host: string): Promise<void> {
  await page.getByRole('button', { name: /Quick connect/ }).click()
  const dialog = page.getByRole('dialog', { name: 'Quick connect' })
  await dialog.getByRole('tab', { name: protocol, exact: true }).click()
  await dialog.getByLabel('Host').fill(host)
  await dialog.getByLabel('Username').fill('sentinel')
  await dialog.getByLabel('Password').fill('sentinel')
  await dialog.getByRole('button', { name: 'Connect' }).click()
}

/**
 * FTP and FTPS against real vsftpd servers.
 *
 * Scenarios:
 * - a plain FTP server is reached from quick connect, which warns that FTP is not encrypted; its folders are listed and
 *   show a dash where permissions would be, since FTP reports none
 * - an FTPS server with a self-signed certificate asks to trust the certificate first, then connects with TLS on the
 *   control and the data channels, uploads a file, hands it back with the same content, and deletes it
 */
test.describe('ftp and ftps', () => {
  test('lists a plain FTP server and shows no permissions', async ({ shell }) => {
    const { page } = shell
    await page.getByRole('button', { name: /Quick connect/ }).click()
    await page.getByRole('dialog').getByRole('tab', { name: 'FTP', exact: true }).click()
    await expect(page.getByRole('dialog').getByRole('note')).toContainText('FTP sends the password and the files without encryption')
    await page.keyboard.press('Escape')

    await quickConnect(page, 'FTP', 'ftp')

    await expect(page.getByRole('status')).toContainText('It works. Keep this server?')
    await page.getByRole('button', { name: 'Not now' }).click()
    await page.getByRole('treeitem', { name: /^ftp not saved/ }).first().click()
    const grid = page.getByRole('grid', { name: 'Files' })
    const upload = grid.getByRole('row', { name: /upload/ })
    await expect(upload).toBeVisible()
    await expect(upload).toContainText('—')
    await expect(upload).not.toContainText('rwx')
  })

  test('asks to trust an FTPS certificate and transfers a file over TLS', async ({ shell }) => {
    test.setTimeout(120_000)
    const { page } = shell
    await quickConnect(page, 'FTPS', 'ftps')

    await expect(page.getByText('Trust this server?')).toBeVisible()
    await expect(page.getByText('tls-certificate')).toBeVisible()
    await expect(page.getByText(/^SHA256:/)).toBeVisible()
    await page.getByRole('button', { name: 'Trust and connect' }).click()
    await expect(page.getByRole('status')).toContainText('It works. Keep this server?')
    await page.getByRole('button', { name: 'Not now' }).click()
    await page.getByRole('treeitem', { name: /^ftps not saved/ }).click()
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(page).toHaveURL(/path=%2Fupload$/)

    const name = `e2e-ftps-${Date.now()}-${Math.floor(Math.random() * 1e6)}.bin`
    const content = Buffer.alloc(1024 * 1024)
    for (let index = 0; index < content.length; index++) {
      content[index] = (index * 13 + (index >> 7)) & 0xff
    }
    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'application/octet-stream', buffer: content })
    const row = grid.getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()

    await row.click()
    const download = page.waitForEvent('download')
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()
    const received = await readFile((await (await download).path()) as string)
    expect(createHash('sha256').update(received).digest('hex')).toBe(createHash('sha256').update(content).digest('hex'))

    await grid.press('Delete')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click()
    await expect(row).toBeHidden()
  })
})
