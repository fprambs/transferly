import { readFile } from 'node:fs/promises'

import { expect, test } from '../fixtures.ts'

/**
 * Downloading a folder from the SFTP server of the isolated test stack.
 *
 * Scenarios:
 * - the inspector downloads a folder as a ZIP named after it, whose bytes hold the names of its files and subfolders, and the
 *   transfers dock lists the archive as a finished download
 * - the folder can also be downloaded file by file: one download per file of the tree, named as the files are
 */
test.describe('folder download', () => {
  test('downloads a folder as a ZIP and file by file', async ({ shell, quickId, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const parent = `e2e-zip-${stamp}`
    const folder = `tree-${stamp}`
    for (const path of [`/upload/${parent}`, `/upload/${parent}/${folder}`, `/upload/${parent}/${folder}/inner`]) {
      expect((await request.post(`${files}/directories`, { headers, data: { path } })).ok()).toBe(true)
    }
    for (const [name, content] of [
      [`top-${stamp}.txt`, 'top level\n'],
      [`inner/deep-${stamp}.txt`, 'nested file\n'],
    ]) {
      const put = await request.put(`${files}/content`, {
        params: { path: `/upload/${parent}/${folder}/${name}`, mode: 'overwrite' },
        data: Buffer.from(content),
        headers: { ...headers, 'Content-Type': 'application/octet-stream' },
      })
      expect(put.ok()).toBe(true)
    }

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${parent}`)
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: new RegExp(folder) }).click()
    const inspector = page.getByRole('complementary', { name: 'Details' })

    const zip = page.waitForEvent('download')
    await inspector.getByRole('button', { name: 'Download as ZIP' }).click()
    const archive = await zip
    expect(archive.suggestedFilename()).toBe(`${folder}.zip`)
    const bytes = await readFile((await archive.path()) as string)
    expect(bytes.subarray(0, 2).toString()).toBe('PK')
    for (const name of [`top-${stamp}.txt`, `inner/`, `inner/deep-${stamp}.txt`]) {
      expect(bytes.includes(Buffer.from(name))).toBe(true)
    }
    const dock = page.getByRole('region', { name: 'Transfers' })
    await dock.getByRole('tab', { name: /Finished/ }).click()
    await expect(dock.getByRole('listitem').filter({ hasText: `${folder}.zip` })).toContainText('Done', { timeout: 30_000 })

    const names: string[] = []
    page.on('download', (download) => names.push(download.suggestedFilename()))
    await inspector.getByRole('button', { name: 'One by one' }).click()
    await expect.poll(() => [...names].sort(), { timeout: 20_000 }).toEqual([`deep-${stamp}.txt`, `top-${stamp}.txt`])

    await request.delete(files, { params: { path: `/upload/${parent}`, recursive: true }, headers })
  })
})
