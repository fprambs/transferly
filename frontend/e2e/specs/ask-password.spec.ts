import { expect, test } from '../fixtures.ts'

/**
 * The logon type that asks for the password when connecting (ADR-0010), against a real SFTP server.
 *
 * Scenarios:
 * - a site saved with "Ask for password" keeps no secret; opening it asks for the password at once, naming the site, and the folder lists
 *   once it is given; a wrong password is refused and asked for again
 * - a wrong password to a real OpenSSH server is reported as refused credentials, not as a server that does not answer
 */
test.describe('ask for password', () => {
  test('asks for the password when opening the site and lists the folder once it is given', async ({ shell, request }) => {
    const { page } = shell
    const name = `Asks ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await dialog.getByLabel('Logon type').selectOption('ask')
    await dialog.getByLabel('Username', { exact: true }).fill('sentinel')
    await expect(dialog.getByLabel('Password', { exact: true })).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const sites = (await (await request.get(`/api/v1/workspaces/${workspaces[0].id}/sites`)).json()) as Array<{ name: string; authMethod: string; secrets: string[] }>
    const saved = sites.find((site) => site.name === name)
    expect(saved).toMatchObject({ authMethod: 'ask', secrets: [] })

    await page.getByRole('treeitem', { name: new RegExp(name) }).click()
    const ask = page.getByRole('dialog', { name: 'Enter the password' })
    await expect(ask).toContainText(name)
    await expect(ask).toContainText('sentinel')
    await expect(ask.getByLabel('Password', { exact: true })).toBeFocused()
    await page.keyboard.type('not the password')
    await page.keyboard.press('Enter')

    const again = page.getByRole('dialog', { name: 'Enter the password' })
    await expect(again.getByRole('alert')).toHaveText('The server refused that password. Enter it again.')
    await again.getByLabel('Password', { exact: true }).fill('sentinel')
    await again.getByRole('button', { name: 'Connect' }).click()
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()
  })

  test('tells a wrong password apart from a server that does not answer', async ({ shell }) => {
    const { page } = shell
    await page.getByRole('button', { name: /Quick connect/ }).click()
    const dialog = page.getByRole('dialog', { name: 'Quick connect' })
    await dialog.getByLabel('Host').fill('sftp')
    await dialog.getByLabel('Username').fill('sentinel')
    await dialog.getByLabel('Password').fill('not the password')
    await dialog.getByRole('button', { name: 'Connect' }).click()
    await expect(dialog.getByRole('alert')).toHaveText('The server refused the username or password.')
  })
})
