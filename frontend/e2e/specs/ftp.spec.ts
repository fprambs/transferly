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
 * - an upload to a plain FTP server that is canceled half way leaves a partial file, resuming it from the dock sends only the rest with the
 *   resume mode, and the file that arrives is byte for byte the one that was chosen
 * - the time zone probe finds that the plain FTP server, which prints its times in the local time of Santiago, is behind UTC by three or four hours
 * - an FTPS server with a self-signed certificate asks to trust the certificate first, then connects with TLS on the
 *   control and the data channels, uploads a file, hands it back with the same content, and deletes it
 */
test.describe('ftp and ftps', () => {
  test.describe.configure({ mode: 'serial' })

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

  test('resumes a canceled upload to an FTP server', async ({ shell, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const quick = await request.post(`/api/v1/workspaces/${workspaces[0].id}/quick-connections`, {
      headers,
      data: { protocol: 'ftp', host: 'ftp', port: 21, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    expect(quick.ok()).toBe(true)
    const { id: quickId } = (await quick.json()) as { id: string }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const name = `e2e-ftp-resume-${Date.now()}-${Math.floor(Math.random() * 1e6)}.bin`
    const content = Buffer.alloc(3 * 1024 * 1024)
    for (let index = 0; index < content.length; index++) {
      content[index] = (index * 17 + (index >> 8)) & 0xff
    }

    await page.goto(`/sites/${quickId}?path=%2Fupload`)
    await expect(page.getByRole('grid', { name: 'Files' }).or(page.getByText('This folder is empty.'))).toBeVisible()
    const client = await page.context().newCDPSession(page)
    await client.send('Network.enable')
    await client.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: 300 * 1024 })
    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'application/octet-stream', buffer: content })

    const dock = page.getByRole('region', { name: 'Transfers' })
    const running = dock.getByRole('listitem').filter({ hasText: name })
    await expect.poll(async () => Number(await running.getByRole('progressbar').getAttribute('aria-valuenow'))).toBeGreaterThanOrEqual(10)
    await running.getByRole('button', { name: `Cancel ${name}` }).click()

    await dock.getByRole('tab', { name: /Finished/ }).click()
    const stopped = dock.getByRole('listitem').filter({ hasText: name })
    await expect(stopped).toContainText('Canceled')
    await client.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 })
    const resumed = page.waitForRequest((request) => request.method() === 'PUT' && request.url().includes('/files/content'))
    await stopped.getByRole('button', { name: `Resume ${name}` }).click()
    const url = new URL((await resumed).url())
    expect(url.searchParams.get('mode')).toBe('resume')
    expect(Number(url.searchParams.get('offset'))).toBeGreaterThan(0)
    expect(Number(url.searchParams.get('offset'))).toBeLessThan(content.length)

    await dock.getByRole('tab', { name: /Finished/ }).click()
    await expect(stopped).toContainText('Done', { timeout: 30_000 })
    const row = page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: new RegExp(name) })
    await row.click()
    const download = page.waitForEvent('download')
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()
    const received = await readFile((await (await download).path()) as string)
    expect(createHash('sha256').update(received).digest('hex')).toBe(createHash('sha256').update(content).digest('hex'))

    await request.delete(files, { params: { path: `/upload/${name}` }, headers })
    await request.delete(`/api/v1/workspaces/${workspaces[0].id}/quick-connections/${quickId}`, { headers })
  })

  test('detects that the FTP server prints its times in another zone', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const quick = await request.post(`/api/v1/workspaces/${workspaces[0].id}/quick-connections`, {
      headers,
      data: { protocol: 'ftp', host: 'ftp', port: 21, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    expect(quick.ok()).toBe(true)
    const { id: quickId } = (await quick.json()) as { id: string }

    await page.goto(`/sites/${quickId}?path=%2Fupload`)
    await expect(page.getByRole('grid', { name: 'Files' }).or(page.getByText('This folder is empty.'))).toBeVisible()
    await page.getByRole('button', { name: 'Detect the time zone of the server' }).click()

    await expect(page.getByRole('status')).toContainText(/The server prints times -0[34]:00 from UTC\./)
    await expect(page.getByRole('status')).toContainText('Save this connection as a site to keep the time zone.')
    await request.delete(`/api/v1/workspaces/${workspaces[0].id}/quick-connections/${quickId}`, { headers })
  })
})
