import { expect, test } from '../fixtures.ts'

/**
 * The second pane and the copy between panes against the SFTP server of the isolated test stack.
 *
 * Scenarios:
 * - the split button opens a second pane on a saved site; a file dragged from the quick connection in the first pane onto
 *   the second pane is copied through the server, shows in the transfers dock as a finished copy, appears in the listing of
 *   the second pane, inside a folder made for it, and holds the same content
 */
test.describe('split view', () => {
  test('copies a file dragged from one pane to the other', async ({ shell, quickId, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const workspace = workspaces[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const created = await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name: `split-${stamp}`, protocol: 'sftp', host: 'sftp', port: 22, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    expect(created.ok()).toBe(true)
    const { id: siteId } = (await created.json()) as { id: string }
    const quickFiles = `/api/v1/workspaces/${workspace}/sites/${quickId}/files`
    const savedFiles = `/api/v1/workspaces/${workspace}/sites/${siteId}/files`
    const name = `e2e-split-${stamp}.txt`
    const folder = `e2e-copies-${stamp}`
    const made = await request.post(`${savedFiles}/directories`, { headers, data: { path: `/upload/${folder}` } })
    expect(made.ok()).toBe(true)
    const content = 'copied between panes\n'
    const source = `e2e-source-${stamp}`
    expect((await request.post(`${quickFiles}/directories`, { headers, data: { path: `/upload/${source}` } })).ok()).toBe(true)
    const upload = await request.put(`${quickFiles}/content`, {
      params: { path: `/upload/${source}/${name}`, mode: 'overwrite' },
      data: Buffer.from(content),
      headers: { ...headers, 'Content-Type': 'application/octet-stream' },
    })
    expect(upload.ok()).toBe(true)

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${source}`)
    await page.getByRole('button', { name: 'Split view' }).click()
    const second = page.getByRole('group', { name: 'Second pane' })
    await second.getByRole('combobox', { name: 'Site of the second pane' }).selectOption({ label: `split-${stamp}` })
    const secondGrid = second.getByRole('grid', { name: 'Files' })
    await secondGrid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(second.getByRole('button', { name: 'upload', exact: true })).toHaveAttribute('aria-current', 'page')
    await secondGrid.getByRole('row', { name: new RegExp(folder) }).dblclick()
    await expect(second.getByRole('button', { name: folder, exact: true })).toHaveAttribute('aria-current', 'page')

    const row = page.getByRole('grid', { name: 'Files' }).first().getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()
    await row.dragTo(second.locator('section'))

    const dock = page.getByRole('region', { name: 'Transfers' })
    await dock.getByRole('tab', { name: /Finished/ }).click()
    await expect(dock.getByRole('listitem').filter({ hasText: name }).filter({ has: page.getByRole('img', { name: 'Copy' }) })).toContainText('Done', { timeout: 30_000 })
    await expect(secondGrid.getByRole('row', { name: new RegExp(name) })).toBeVisible()
    const copy = await request.get(`${savedFiles}/content`, { params: { path: `/upload/${folder}/${name}` } })
    expect(await copy.text()).toBe(content)

    for (const [files, path] of [[quickFiles, `/upload/${source}`], [savedFiles, `/upload/${folder}`]] as const) {
      await request.delete(files, { params: { path, recursive: true }, headers })
    }
    await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}`, { headers })
  })
})
