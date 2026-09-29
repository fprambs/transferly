import { expect, test } from '../fixtures.ts'

/**
 * Bookmarks of a saved site (4.14b) against the SFTP server of the isolated stack.
 *
 * Scenarios:
 * - the folder in view is bookmarked with its name suggested, the icon fills, and from the root the bookmark opens that folder again
 * - the bookmark is kept on the server: it is still there after a reload, and removing it from the manage dialog leaves none
 */
test.describe('bookmarks', () => {
  test('bookmarks a folder, opens it again and removes it', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspace = ((await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>)[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const name = `Bookmarked ${Date.now()}`
    const created = await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name, protocol: 'sftp', host: 'sftp', port: 22, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    expect(created.ok()).toBe(true)
    const { id: siteId } = (await created.json()) as { id: string }
    const bookmarks = page.getByRole('button', { name: 'Bookmarks' })

    await page.goto(`/sites/${siteId}?path=%2Fupload`)
    await expect(page.getByRole('navigation', { name: 'Path' }).getByRole('button', { name: 'upload' })).toBeVisible()
    await bookmarks.click()
    await page.getByRole('menuitem', { name: 'Bookmark this folder' }).click()
    const dialog = page.getByRole('dialog', { name: 'Bookmark this folder' })
    await expect(dialog.getByLabel('Name')).toHaveValue('upload')
    await dialog.getByRole('button', { name: 'Add bookmark' }).click()
    await expect(dialog).toBeHidden()
    await expect(bookmarks.locator('svg')).toHaveClass(/fill-warn/)

    await page.getByRole('button', { name: 'Up one level' }).click()
    await expect(page).toHaveURL(/path=%2F(&|$)/)
    await expect(bookmarks.locator('svg')).not.toHaveClass(/fill-warn/)
    await page.reload()
    await bookmarks.click()
    await page.getByRole('menuitem', { name: /upload/ }).click()
    await expect(page).toHaveURL(/path=%2Fupload/)

    await bookmarks.click()
    await page.getByRole('menuitem', { name: 'Manage bookmarks…' }).click()
    const manage = page.getByRole('dialog', { name: 'Manage bookmarks' })
    await manage.getByRole('button', { name: 'Remove upload' }).click()
    await expect(manage.getByText('No bookmarks on this site yet.')).toBeVisible()

    await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}`, { headers })
  })
})
