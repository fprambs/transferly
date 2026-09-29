import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/** What the page offers the test to reach the folder a fake picker answers with. */
interface FolderTools {
  /** Reads the text of a file of the folder. */
  read: (name: string) => Promise<string>
}

/**
 * Script that gives the page a folder picker answering with a folder kept in memory, shaped like the handles of the browser. The test
 * server is not a secure context, where the real picker and the private file system do not exist.
 */
const folderScript = `
  const files = new Map()
  const handleOf = (name) => {
    const entry = files.get(name)
    return {
      kind: 'file',
      name,
      async getFile() { return new File(entry.parts, name, { lastModified: entry.modified }) },
      async createWritable() {
        const parts = []
        return new WritableStream({ write(chunk) { parts.push(chunk) }, close() { entry.parts = parts; entry.modified = Date.now() } })
      },
    }
  }
  const folder = {
    kind: 'directory',
    name: 'e2e-sync-folder',
    async getDirectoryHandle(name) { throw new DOMException(name, 'NotFoundError') },
    async getFileHandle(name, options) {
      if (!files.has(name)) {
        if (!options || !options.create) throw new DOMException(name, 'NotFoundError')
        files.set(name, { parts: [], modified: Date.now() })
      }
      return handleOf(name)
    },
    async removeEntry(name) { files.delete(name) },
    async *entries() {
      for (const name of [...files.keys()]) yield [name, handleOf(name)]
    },
  }
  window.folderTools = { async read(name) { return (await handleOf(name).getFile()).text() } }
  window.showDirectoryPicker = async () => folder
`

/**
 * Comparing and syncing two folders against the SFTP server of the isolated test stack.
 *
 * Scenarios:
 * - the compare button opens once the second pane has something to compare, lists a file only on each side and one that differs, and
 *   syncing the checked rows copies the missing file across and updates the one that differed, leaving the server holding it
 * - checking a delete row asks to confirm, and once confirmed removes that file from the target
 * - comparing a remote folder against a local one downloads the file missing locally into the chosen folder
 */
test.describe('compare and sync', () => {
  test('lists the differences between two saved sites and copies what is checked', async ({ shell, quickId, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const workspace = workspaces[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const created = await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name: `sync-${stamp}`, protocol: 'ftp', host: 'ftp', port: 21, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    expect(created.ok()).toBe(true)
    const { id: siteId } = (await created.json()) as { id: string }
    const leftFiles = `/api/v1/workspaces/${workspace}/sites/${quickId}/files`
    const rightFiles = `/api/v1/workspaces/${workspace}/sites/${siteId}/files`
    const folder = `e2e-sync-${stamp}`
    for (const files of [leftFiles, rightFiles]) {
      expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })).ok()).toBe(true)
    }
    const write = async (files: string, path: string, content: string) =>
      (await request.put(`${files}/content`, { params: { path, mode: 'overwrite' }, data: Buffer.from(content), headers: { ...headers, 'Content-Type': 'application/octet-stream' } })).ok()
    const onlyLeft = `only-left-${stamp}.txt`
    const onlyRight = `only-right-${stamp}.txt`
    const changed = `changed-${stamp}.txt`
    expect(await write(leftFiles, `/upload/${folder}/${onlyLeft}`, 'from the left\n')).toBe(true)
    expect(await write(rightFiles, `/upload/${folder}/${onlyRight}`, 'from the right\n')).toBe(true)
    expect(await write(leftFiles, `/upload/${folder}/${changed}`, 'the left version, longer\n')).toBe(true)
    expect(await write(rightFiles, `/upload/${folder}/${changed}`, 'right\n')).toBe(true)

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${folder}`)
    await page.getByRole('button', { name: 'Split view' }).click()
    const second = page.getByRole('group', { name: 'Second pane' })
    await choose(second.getByRole('combobox', { name: 'Site of the second pane' }), `sync-${stamp}`)
    await second.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ }).dblclick()
    await second.getByRole('grid', { name: 'Files' }).getByRole('row', { name: new RegExp(folder) }).dblclick()
    await expect(second.getByRole('button', { name: folder, exact: true })).toHaveAttribute('aria-current', 'page')

    await second.getByRole('button', { name: 'Compare and sync with the other pane' }).click()
    const dialog = page.getByRole('dialog', { name: 'Compare and sync' })
    const list = dialog.getByRole('list', { name: 'Comparison' })
    await expect(list.getByText(onlyLeft)).toBeVisible()
    await expect(list.getByText(onlyRight)).toBeVisible()
    await expect(list.getByText(changed)).toBeVisible()
    await expect(dialog.getByRole('checkbox', { name: `Include ${onlyRight} in the sync` })).not.toBeChecked()

    await dialog.getByRole('button', { name: 'Sync 2 files' }).click()
    await expect(dialog.getByText('Done')).toHaveCount(2)
    const uploaded = await request.get(`${rightFiles}/content`, { params: { path: `/upload/${folder}/${onlyLeft}` } })
    expect(uploaded.ok()).toBe(true)
    const synced = await request.get(`${rightFiles}/content`, { params: { path: `/upload/${folder}/${changed}` } })
    expect(await synced.text()).toBe('the left version, longer\n')

    await dialog.getByRole('checkbox', { name: `Include ${onlyRight} in the sync` }).check()
    await dialog.getByRole('button', { name: 'Sync 1 file' }).click()
    await expect(dialog.getByRole('alert')).toContainText(`This will permanently remove 1 file from sync-${stamp}`)
    await dialog.getByRole('button', { name: 'Yes, sync and delete' }).click()
    await expect(dialog.getByText('Done')).toHaveCount(3)
    const removed = await request.get(`${rightFiles}/content`, { params: { path: `/upload/${folder}/${onlyRight}` } })
    expect(removed.status()).toBe(404)

    await dialog.getByRole('button', { name: 'Close' }).click()
    for (const files of [leftFiles, rightFiles]) {
      await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
    }
    await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}`, { headers })
  })

  test('downloads a file missing from a local folder into it', async ({ shell, quickId, request }) => {
    const { page } = shell
    await page.addInitScript(folderScript)
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const folder = `e2e-sync-local-${stamp}`
    const name = `remote-only-${stamp}.txt`
    expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })).ok()).toBe(true)
    expect(
      (
        await request.put(`${files}/content`, {
          params: { path: `/upload/${folder}/${name}`, mode: 'overwrite' },
          data: Buffer.from('only on the server\n'),
          headers: { ...headers, 'Content-Type': 'application/octet-stream' },
        })
      ).ok(),
    ).toBe(true)

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${folder}&split=local`)
    const second = page.getByRole('group', { name: 'Second pane' })
    await second.getByRole('button', { name: 'Choose a folder' }).click()
    await expect(second.getByText('This folder is empty.')).toBeVisible()

    await second.getByRole('button', { name: 'Compare and sync with the other pane' }).click()
    const dialog = page.getByRole('dialog', { name: 'Compare and sync' })
    await expect(dialog.getByRole('list', { name: 'Comparison' }).getByText(name)).toBeVisible()
    await dialog.getByRole('button', { name: 'Sync 1 file' }).click()
    await expect(dialog.getByText('Done')).toBeVisible()

    const saved = await page.evaluate((fileName) => (globalThis as unknown as { folderTools: FolderTools }).folderTools.read(fileName), name)
    expect(saved).toBe('only on the server\n')

    await dialog.getByRole('button', { name: 'Close' }).click()
    await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
  })
})
