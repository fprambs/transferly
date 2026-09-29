import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { expect, test } from '../fixtures.ts'

/**
 * Importing and exporting sites in the isolated test stack.
 *
 * Scenarios:
 * - a site manager file with a folder, an SFTP site with a saved password and a protocol that is not supported is previewed, then imported
 *   with its password: the folder and the site appear in the sites tree, the site keeps its password sealed in the vault, and the
 *   unsupported one is reported as skipped
 * - the sites exported to an encrypted file (ADR-0017) download as a file that shows none of them, and it opens again with its password:
 *   its preview lists the sites of the workspace, skipped because their names are taken; a wrong password is refused
 */
test.describe('import and export sites', () => {
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
    await page.getByRole('button', { name: 'Import sites' }).click()
    const dialog = page.getByRole('dialog', { name: 'Import sites' })
    await dialog.getByLabel('File to import').setInputFiles(file)
    await expect(dialog.getByText('FileZilla site manager')).toBeVisible()
    await dialog.getByRole('checkbox').check()
    await dialog.getByRole('button', { name: 'Preview' }).click()
    await expect(dialog.getByText('1 sites will be created, 1 skipped.')).toBeVisible()
    await expect(dialog.getByRole('listitem').filter({ hasText: `Web ${stamp}` })).toContainText('Protocol not supported')

    await dialog.getByRole('button', { name: 'Import', exact: true }).click()
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

  test('exports the sites to an encrypted file that opens again with its password', async ({ shell }) => {
    const { page } = shell
    const name = `Exported ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const form = page.getByRole('dialog', { name: 'New site' })
    await form.getByLabel('Name', { exact: true }).fill(name)
    await form.getByLabel('Host', { exact: true }).fill('sftp')
    await form.getByLabel('Username', { exact: true }).fill('sentinel')
    await form.getByLabel('Password', { exact: true }).fill('sentinel')
    await form.getByRole('button', { name: 'Create site' }).click()
    await expect(form).toBeHidden()

    await page.getByRole('button', { name: 'Export sites' }).click()
    const exporting = page.getByRole('dialog', { name: 'Export sites' })
    await exporting.getByLabel('File password').fill('a long export password')
    await exporting.getByLabel('Repeat the password').fill('a long export password')
    const download = page.waitForEvent('download')
    await exporting.getByRole('button', { name: 'Export' }).click()
    const file = await (await download).path()
    await expect(exporting.getByRole('status')).toContainText('sentinel-sites-')
    await exporting.getByRole('button', { name: 'Close' }).click()
    const text = await readFile(file, 'utf8')
    expect(text).toContain('"format": "sentinel-export"')
    expect(text).not.toContain(name)

    await page.getByRole('button', { name: 'Import sites' }).click()
    const dialog = page.getByRole('dialog', { name: 'Import sites' })
    await dialog.getByLabel('File to import').setInputFiles(file)
    await expect(dialog.getByText('Sentinel export, encrypted')).toBeVisible()
    await dialog.getByLabel('File password').fill('not the export password')
    await dialog.getByRole('button', { name: 'Preview' }).click()
    await expect(dialog.getByRole('alert')).toContainText('That password does not open the file, or the file was changed.')
    await dialog.getByLabel('File password').fill('a long export password')
    await dialog.getByRole('button', { name: 'Preview' }).click()
    await expect(dialog.getByRole('listitem').filter({ hasText: name })).toContainText('Name already used')
  })
})
