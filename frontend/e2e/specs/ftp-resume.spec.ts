import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'

import { expect, test } from '../fixtures.ts'

/**
 * Resuming an upload to a plain FTP server of the isolated test stack.
 *
 * Scenarios:
 * - an upload to an FTP server that is canceled half way leaves a partial file, resuming it from the dock sends only the rest with the
 *   resume mode, and the file that arrives is byte for byte the one that was chosen
 */
test.describe('ftp resume', () => {
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
})
