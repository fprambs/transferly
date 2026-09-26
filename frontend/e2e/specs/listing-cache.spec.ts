import { expect, test } from '../fixtures.ts'

/**
 * The cache of folder listings against the SFTP server of the isolated test stack.
 *
 * Scenarios:
 * - going back to a folder that was just visited shows its listing without a new request to the server, and resting the pointer on a
 *   folder fetches its listing before it is opened
 */
test.describe('listing cache', () => {
  test('does not ask the server again for a folder that was just seen', async ({ shell, quickId, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const parent = `e2e-cache-${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    for (const path of [`/upload/${parent}`, `/upload/${parent}/child`, `/upload/${parent}/other`]) {
      expect((await request.post(`${files}/directories`, { headers, data: { path } })).ok()).toBe(true)
    }
    const asked: string[] = []
    await page.route('**/files?**', async (route) => {
      const url = new URL(route.request().url())
      if (route.request().method() === 'GET' && !url.pathname.endsWith('/content')) {
        asked.push(url.searchParams.get('path') as string)
      }
      await route.continue()
    })

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${parent}`)
    const grid = page.getByRole('grid', { name: 'Files' })
    await expect(grid.getByRole('row', { name: /child/ })).toBeVisible()
    await grid.getByRole('row', { name: /other/ }).hover()
    await expect.poll(() => asked.filter((path) => path === `/upload/${parent}/other`).length).toBe(1)
    await grid.getByRole('row', { name: /other/ }).dblclick()
    await expect(page).toHaveURL(new RegExp(`other$`))
    await page.getByRole('button', { name: 'Up one level' }).click()
    await expect(grid.getByRole('row', { name: /child/ })).toBeVisible()

    expect(asked.filter((path) => path === `/upload/${parent}`)).toHaveLength(1)
    expect(asked.filter((path) => path === `/upload/${parent}/other`)).toHaveLength(1)
    await request.delete(files, { params: { path: `/upload/${parent}`, recursive: true }, headers })
  })
})
