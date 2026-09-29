import { expect, test } from '../fixtures.ts'

/** What the page offers the test to reach the folder that the picker answers with. */
interface FolderTools {
  /** Reads the text of a file of the folder. */
  read: (name: string) => Promise<string>
  /** Creates or replaces a file of the folder. */
  write: (name: string, text: string) => void
  /** Removes a file of the folder. */
  remove: (name: string) => void
}

/**
 * Script that gives the page a folder picker answering with a folder kept in memory, shaped like the handles of the
 * browser. The test server is not a secure context, where the real picker and the private file system do not exist.
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
    name: 'e2e-folder',
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
  window.folderTools = {
    async read(name) { return (await handleOf(name).getFile()).text() },
    write(name, text) { files.set(name, { parts: [text], modified: Date.now() }) },
    remove(name) { files.delete(name) },
  }
  window.showDirectoryPicker = async () => folder
`

/**
 * The local folder pane against the SFTP server of the isolated test stack.
 *
 * The browser's folder dialog cannot be driven by a test, so the picker is replaced by one that answers with a folder kept
 * in memory.
 *
 * Scenarios:
 * - a file dragged from the server onto the local pane is saved in the chosen folder with the same content, and listed; the headers of
 *   the local table sit on one line
 * - a file of the chosen folder dragged onto the server pane is uploaded, listed there and shown as a finished upload
 */
test.describe('local folder pane', () => {
  test('exchanges files between the server and a local folder by dragging', async ({ shell, quickId, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    await page.addInitScript(folderScript)
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const down = `e2e-down-${stamp}.txt`
    const up = `e2e-up-${stamp}.txt`
    const source = `e2e-source-${stamp}`
    expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${source}` } })).ok()).toBe(true)
    const put = await request.put(`${files}/content`, {
      params: { path: `/upload/${source}/${down}`, mode: 'overwrite' },
      data: Buffer.from('from the server\n'),
      headers: { ...headers, 'Content-Type': 'application/octet-stream' },
    })
    expect(put.ok()).toBe(true)

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${source}&split=local`)
    const local = page.getByRole('group', { name: 'Second pane' })
    await local.getByRole('button', { name: 'Choose a folder' }).click()
    await expect(local.getByText('This folder is empty.')).toBeVisible()

    const remoteGrid = page.getByRole('grid', { name: 'Files' })
    await remoteGrid.getByRole('row', { name: new RegExp(down) }).dragTo(local.getByRole('region', { name: 'Local folder' }))
    const localGrid = local.getByRole('grid', { name: 'Files' })
    await expect(localGrid.getByRole('row', { name: new RegExp(down) })).toBeVisible()
    const tops = await localGrid.getByRole('columnheader').evaluateAll((cells) =>
      cells.filter((cell) => cell.getClientRects().length > 0).map((cell) => Math.round(cell.getBoundingClientRect().top)),
    )
    expect(new Set(tops).size, `header rows at ${JSON.stringify(tops)}`).toBe(1)
    const saved = await page.evaluate((name) => (globalThis as unknown as { folderTools: FolderTools }).folderTools.read(name), down)
    expect(saved).toBe('from the server\n')

    await page.evaluate((name) => (globalThis as unknown as { folderTools: FolderTools }).folderTools.write(name, 'from the computer\n'), up)
    await local.getByRole('button', { name: 'Refresh' }).click()
    await expect(localGrid.getByRole('row', { name: new RegExp(up) })).toBeVisible()
    await localGrid.getByRole('row', { name: new RegExp(up) }).dragTo(page.getByRole('region', { name: 'Files' }).first())

    await expect(remoteGrid.getByRole('row', { name: new RegExp(up) })).toBeVisible()
    const uploaded = await request.get(`${files}/content`, { params: { path: `/upload/${source}/${up}` } })
    expect(await uploaded.text()).toBe('from the computer\n')

    await page.evaluate((names) => names.forEach((name) => (globalThis as unknown as { folderTools: FolderTools }).folderTools.remove(name)), [down, up])
    await request.delete(files, { params: { path: `/upload/${source}`, recursive: true }, headers })
  })
})
