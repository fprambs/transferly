import { expect, test } from '../fixtures.ts'

/**
 * Searching a folder recursively against the SFTP server of the isolated test stack.
 *
 * Scenarios:
 * - a search finds a file nested inside a subfolder and opens the folder that holds it
 * - a glob pattern matches only the names it should
 * - stopping a search keeps what was already found
 */
test.describe('search', () => {
  test('finds a file below a subfolder and opens its folder', async ({ shell, quickId, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const folder = `e2e-search-${stamp}`
    const sub = `${folder}/nested`
    const name = `deep-report-${stamp}.csv`
    expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })).ok()).toBe(true)
    expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${sub}` } })).ok()).toBe(true)
    expect(
      (
        await request.put(`${files}/content`, {
          params: { path: `/upload/${sub}/${name}`, mode: 'overwrite' },
          data: Buffer.from('a,b\n1,2\n'),
          headers: { ...headers, 'Content-Type': 'application/octet-stream' },
        })
      ).ok(),
    ).toBe(true)

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${folder}`)
    await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Search' })
    await dialog.getByLabel('Search query').fill('deep-report')
    await dialog.getByRole('button', { name: 'Search' }).click()

    const results = dialog.getByRole('list', { name: 'Search results' })
    await expect(results.getByText(`/upload/${sub}/${name}`)).toBeVisible()
    await results.getByText(`/upload/${sub}/${name}`).click()

    await expect(page.getByRole('button', { name: 'nested', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: new RegExp(name) })).toBeVisible()

    await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
  })

  test('matches a glob pattern against the whole name', async ({ shell, quickId, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const folder = `e2e-search-glob-${stamp}`
    await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })
    const write = async (name: string) => request.put(`${files}/content`, { params: { path: `/upload/${folder}/${name}`, mode: 'overwrite' }, data: Buffer.from('x'), headers: { ...headers, 'Content-Type': 'application/octet-stream' } })
    await write(`report-${stamp}.csv`)
    await write(`report-${stamp}.csv.bak`)

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${folder}`)
    await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Search' })
    await dialog.getByLabel('Search query').fill('*.csv')
    await dialog.getByRole('button', { name: 'Search' }).click()

    const results = dialog.getByRole('list', { name: 'Search results' })
    await expect(results.getByText(`/upload/${folder}/report-${stamp}.csv`, { exact: true })).toBeVisible()
    await expect(results.getByText(`/upload/${folder}/report-${stamp}.csv.bak`)).toHaveCount(0)

    await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
  })

  test('keeps what a search already found when it is stopped', async ({ shell, quickId, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const folder = `e2e-search-stop-${stamp}`
    await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })
    for (let i = 0; i < 15; i += 1) {
      const sub = `${folder}/sub-${i}`
      await request.post(`${files}/directories`, { headers, data: { path: `/upload/${sub}` } })
      await request.put(`${files}/content`, { params: { path: `/upload/${sub}/note-${stamp}.txt`, mode: 'overwrite' }, data: Buffer.from('x'), headers: { ...headers, 'Content-Type': 'application/octet-stream' } })
    }

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${folder}`)
    await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Search' })
    await dialog.getByLabel('Search query').fill('note')
    await dialog.getByRole('button', { name: 'Search' }).click()
    await expect(dialog.getByRole('list', { name: 'Search results' }).getByRole('listitem').first()).toBeVisible()
    const stop = dialog.getByRole('button', { name: 'Stop', exact: true })
    if (await stop.isVisible()) {
      await stop.click()
    }

    await expect(dialog.getByText(/match(es)? found/)).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Search', exact: true })).toBeVisible()
    const found = await dialog.getByRole('list', { name: 'Search results' }).getByRole('listitem').count()
    expect(found).toBeGreaterThan(0)
    expect(found).toBeLessThanOrEqual(15)

    await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
  })
})
