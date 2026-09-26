import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { expect, test } from '../fixtures.ts'

/**
 * Importing the site manager of FileZilla in the isolated test stack.
 *
 * Scenarios:
 * - a site manager file with a folder, an SFTP site with a saved password and a protocol that is not supported is previewed, then imported
 *   with its password: the folder and the site appear in the sites tree, the site keeps its password sealed in the vault, and the
 *   unsupported one is reported as skipped
 */
test.describe('import from FileZilla', () => {
  test('previews and imports a site manager file', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const folder = `Imported ${stamp}`
    const name = `Box ${stamp}`
    const xml = `<?xml version="1.0" encoding="UTF-8"?>
<FileZilla3><Servers>
  <Folder>${folder}
    <Server><Host>sftp</Host><Port>22</Port><Protocol>1</Protocol><Logontype>1</Logontype><User>sentinel</User><Pass encoding="base64">${Buffer.from('sentinel').toString('base64')}</Pass><Name>${name}</Name></Server>
    <Server><Host>web.example.com</Host><Protocol>2</Protocol><Logontype>1</Logontype><User>x</User><Name>Web ${stamp}</Name></Server>
  </Folder>
</Servers></FileZilla3>`
    const dir = await mkdtemp(join(tmpdir(), 'sentinel-import-'))
    const file = join(dir, 'sitemanager.xml')
    await writeFile(file, xml)

    await page.goto('/')
    await page.getByRole('button', { name: 'Import from FileZilla' }).click()
    const dialog = page.getByRole('dialog', { name: 'Import from FileZilla' })
    await dialog.getByLabel('Site manager file').setInputFiles(file)
    await dialog.getByRole('checkbox').check()
    await dialog.getByRole('button', { name: 'Preview' }).click()
    await expect(dialog.getByText('1 sites will be created, 1 skipped.')).toBeVisible()
    await expect(dialog.getByRole('listitem').filter({ hasText: `Web ${stamp}` })).toContainText('Protocol not supported')

    await dialog.getByRole('button', { name: 'Import' }).click()
    await expect(dialog.getByText('1 sites created, 1 skipped, 0 failed.')).toBeVisible()
    await dialog.getByRole('button', { name: 'Close' }).click()

    const tree = page.getByRole('tree', { name: 'Sites' })
    await expect(tree.getByRole('treeitem', { name: new RegExp(folder) })).toBeVisible()
    const sites = (await (await request.get(`/api/v1/workspaces/${workspaces[0].id}/sites`)).json()) as Array<{ id: string; name: string; secrets: string[] }>
    const created = sites.find((site) => site.name === name)
    expect(created?.secrets).toEqual(['password'])

    await request.delete(`/api/v1/workspaces/${workspaces[0].id}/sites/${created?.id}`, { headers })
    const collections = (await (await request.get(`/api/v1/workspaces/${workspaces[0].id}/collections`)).json()) as Array<{ id: string; name: string }>
    const imported = collections.find((collection) => collection.name === folder)
    if (imported !== undefined) {
      await request.delete(`/api/v1/workspaces/${workspaces[0].id}/collections/${imported.id}`, { headers })
    }
    await rm(dir, { recursive: true, force: true })
  })
})
