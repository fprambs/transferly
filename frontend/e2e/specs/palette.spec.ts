import { expect, test } from '../fixtures.ts'

/**
 * The command palette (4.14c) against the SFTP server of the isolated stack.
 *
 * Scenarios:
 * - Ctrl+K opens it, a search by host finds a saved site and Enter connects it
 * - on the open site, running "Filter this folder" focuses the filter box, and the panels are hidden and shown from it
 */
test.describe('command palette', () => {
  test('finds a site, connects it and runs commands', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspace = ((await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>)[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const name = `Palette ${Date.now()}`
    const created = await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name, protocol: 'sftp', host: 'sftp', port: 22, username: 'transferly', authMethod: 'password', password: 'transferly' },
    })
    const { id: siteId } = (await created.json()) as { id: string }
    await page.reload()
    await expect(page.getByRole('button', { name: /Search or run a command/ })).toContainText('Ctrl+K')

    await page.keyboard.press('Control+k')
    const palette = page.getByRole('dialog', { name: 'Command palette' })
    await expect(palette).toBeVisible()
    await palette.getByRole('combobox').fill(name)
    await expect(palette.getByRole('option', { name: new RegExp(name) })).toBeVisible()
    await page.keyboard.press('Enter')
    await expect(palette).toBeHidden()
    await expect(page).toHaveURL(new RegExp(`/sites/${siteId}`))
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()

    await page.keyboard.press('Control+k')
    await palette.getByRole('combobox').fill('filter this')
    await page.keyboard.press('Enter')
    await expect(page.getByRole('searchbox', { name: 'Filter this folder' })).toBeFocused()

    await page.keyboard.press('Control+k')
    await palette.getByRole('option', { name: /Hide the log panel/ }).click()
    await expect(page.getByRole('list', { name: 'Connection log' })).toBeHidden()
    await page.keyboard.press('Control+k')
    await palette.getByRole('option', { name: /Show the log panel/ }).click()
    await expect(page.getByRole('list', { name: 'Connection log' })).toBeVisible()

    await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}`, { headers })
  })
})
