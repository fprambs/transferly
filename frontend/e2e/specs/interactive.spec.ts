import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/** Saves an SFTP site of the interactive logon type pointing at the test server, and returns its name. */
async function saveInteractiveSite(page: Page, prefix: string): Promise<string> {
  const name = `${prefix} ${Date.now()}`
  await page.getByRole('button', { name: 'New site' }).click()
  const dialog = page.getByRole('dialog', { name: 'New site' })
  await dialog.getByRole('button', { name: /^SFTP/ }).click()
  await dialog.getByLabel('Name', { exact: true }).fill(name)
  await dialog.getByLabel('Host', { exact: true }).fill('sftp')
  await dialog.getByLabel('Username', { exact: true }).fill('transferly')
  await choose(dialog.getByLabel('Logon type'), 'Interactive')
  await expect(dialog.getByLabel('Password', { exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Create site' }).click()
  await expect(dialog).toBeHidden()
  return name
}

/**
 * The interactive logon type (ADR-0016), against a real OpenSSH server: whatever the server asks while signing in reaches the browser live.
 *
 * Scenarios:
 * - a site saved as Interactive keeps no secret; opening it shows what the server asks, naming the site, with the answer hidden, and the
 *   folder lists once it is answered; the connection log tells that the server asked
 * - declining the question fails the connection as refused credentials
 */
test.describe('interactive logon', () => {
  test('answers what the server asks and lists the folder', async ({ shell, request }) => {
    const { page } = shell
    const name = await saveInteractiveSite(page, 'Interactive')
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const sites = (await (await request.get(`/api/v1/workspaces/${workspaces[0].id}/sites`)).json()) as Array<{ name: string; config: Record<string, string>; secrets: string[] }>
    expect(sites.find((site) => site.name === name)).toMatchObject({ config: { auth: 'interactive' }, secrets: [] })

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const prompt = page.getByRole('dialog', { name: `${name} asks` })
    await expect(prompt).toContainText('sftp')
    const answer = prompt.getByLabel('Password:')
    await expect(answer).toBeFocused()
    await expect(answer).toHaveAttribute('type', 'password')
    await page.keyboard.type('transferly')
    await page.keyboard.press('Enter')

    await expect(prompt).toBeHidden()
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()
    await expect(page.getByRole('list', { name: 'Connection log' })).toContainText('The server asks for an answer.')
  })

  test('fails as refused credentials when the question is declined', async ({ shell }) => {
    const { page } = shell
    const name = await saveInteractiveSite(page, 'Declined')
    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const prompt = page.getByRole('dialog', { name: `${name} asks` })
    await prompt.getByRole('button', { name: 'Cancel' }).click()
    await expect(prompt).toBeHidden()
    await expect(page.getByText('The server refused the credentials.').first()).toBeVisible()
  })
})
