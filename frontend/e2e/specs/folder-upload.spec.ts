import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { expect, test } from '../fixtures.ts'

/**
 * Uploading a folder from the computer to the SFTP server of the isolated test stack.
 *
 * Scenarios:
 * - the upload folder button takes a folder with a subfolder, asks what to do with files that exist and creates the tree on the
 *   server, with the contents of its files, and the transfers dock lists the uploads as done; the browser reports no empty
 *   folders through this button, so they are only created when the folder is dropped
 */
test.describe('folder upload', () => {
  test('uploads a folder with its subfolders through the folder button', async ({ shell, quickId, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const parent = `e2e-folder-${stamp}`
    expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${parent}` } })).ok()).toBe(true)

    const local = await mkdtemp(join(tmpdir(), 'sentinel-e2e-'))
    const tree = join(local, `tree-${stamp}`)
    await mkdir(join(tree, 'sub'), { recursive: true })
    await writeFile(join(tree, 'top.txt'), 'top of the tree\n')
    await writeFile(join(tree, 'sub', 'nested.txt'), 'nested in the tree\n')

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${parent}`)
    await expect(page.getByRole('grid', { name: 'Files' }).or(page.getByText('This folder is empty.'))).toBeVisible()
    await page.getByLabel('Choose a folder to upload').setInputFiles(tree)
    const dialog = page.getByRole('dialog', { name: `Copy tree-${stamp}` })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Copy' }).click()

    const dock = page.getByRole('region', { name: 'Transfers' })
    await expect(dock.getByRole('listitem').filter({ hasText: 'nested.txt' }).first()).toContainText('Done', { timeout: 30_000 })
    await expect(dock.getByRole('listitem').filter({ hasText: 'top.txt' }).first()).toContainText('Done', { timeout: 30_000 })
    const top = await request.get(`${files}/content`, { params: { path: `/upload/${parent}/tree-${stamp}/top.txt` } })
    expect(await top.text()).toBe('top of the tree\n')
    const nested = await request.get(`${files}/content`, { params: { path: `/upload/${parent}/tree-${stamp}/sub/nested.txt` } })
    expect(await nested.text()).toBe('nested in the tree\n')
    const listing = (await (await request.get(files, { params: { path: `/upload/${parent}/tree-${stamp}` } })).json()) as Array<{ name: string }>
    expect(listing.map((item) => item.name).sort()).toEqual(['sub', 'top.txt'])

    await request.delete(files, { params: { path: `/upload/${parent}`, recursive: true }, headers })
    await rm(local, { recursive: true, force: true })
  })
})
