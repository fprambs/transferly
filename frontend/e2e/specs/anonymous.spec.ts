import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/**
 * The anonymous logon type over SFTP, against a real OpenSSH server that lets the anonymous user in with the SSH "none" method.
 *
 * Scenarios:
 * - a site saved with the Anonymous logon type keeps no secret and signs in as anonymous; opening it lists the public folder with no
 *   password asked for and no vault involved
 */
test.describe('anonymous sftp', () => {
  test('signs in to a public SFTP server with no secret', async ({ shell, request }) => {
    const { page } = shell
    const name = `Public SFTP ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await choose(dialog.getByLabel('Logon type'), 'Anonymous')
    await expect(dialog.getByLabel('Username', { exact: true })).toHaveValue('anonymous')
    await expect(dialog.getByLabel('Password', { exact: true })).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const sites = (await (await request.get(`/api/v1/workspaces/${workspaces[0].id}/sites`)).json()) as Array<{ name: string; config: Record<string, string>; secrets: string[] }>
    expect(sites.find((site) => site.name === name)).toMatchObject({ config: { auth: 'anonymous', username: 'anonymous' }, secrets: [] })

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /pub/ }).dblclick()
    await expect(grid.getByRole('row', { name: /readme\.txt/ })).toBeVisible()
  })
})
