import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/**
 * The live connection log (ADR-0011), against the real servers of the isolated stack.
 *
 * Scenarios:
 * - opening an SFTP site tells, in order, the connection, the address reached, the verified server key, the account that signed in and the
 *   listing with its count, and the password never shows
 * - after disconnecting, opening the site again starts the log from the first step instead of carrying on the previous session
 * - an FTP site with optional encryption, against a server without TLS, warns that it went on in clear text
 */
test.describe('connection log', () => {
  test('tells each step of opening a site, in order', async ({ shell }) => {
    const { page } = shell
    const name = `Logged ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await dialog.getByLabel('Username', { exact: true }).fill('transferly')
    await dialog.getByLabel('Password', { exact: true }).fill('transferly')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const log = page.getByRole('list', { name: 'Connection log' })
    await expect(log.getByText(/^Listed \/: \d+ entries\.$/)).toBeVisible()
    const lines = await log.getByRole('listitem').locator('span').allTextContents()
    const order = [/^Connecting to sftp:22…$/, /^Connected to /, /^Server key verified: /, /^Signed in as transferly \(password\)\.$/, /^Listing \/…$/, /^Listed \//]
    let at = 0
    for (const line of lines) {
      if (at < order.length && order[at].test(line)) {
        at++
      }
    }
    expect(at, `steps in order within ${JSON.stringify(lines)}`).toBe(order.length)

    await page.getByRole('button', { name: 'Disconnect from the server' }).click()
    await expect(page).toHaveURL(/\/$/)
    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    await expect(log.getByText(/^Listed \/: \d+ entries\.$/)).toBeVisible()
    const again = await log.getByRole('listitem').locator('span').allTextContents()
    expect(again[0]).toBe('Connecting to sftp:22…')
    expect(again.filter((line) => line.startsWith('Connecting to'))).toHaveLength(1)
  })

  test('warns when FTP goes on without TLS', async ({ shell }) => {
    const { page } = shell
    const name = `Clear text ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await choose(dialog.getByLabel('Protocol'), 'FTP - File Transfer Protocol')
    await choose(dialog.getByLabel('Encryption'), 'Use explicit FTP over TLS if available')
    await dialog.getByLabel('Host', { exact: true }).fill('ftp')
    await choose(dialog.getByLabel('Logon type'), 'Anonymous')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const warning = page.getByRole('list', { name: 'Connection log' }).getByText('The server offers no TLS: going on in clear text, the password included.')
    await expect(warning).toBeVisible()
    await expect(warning.locator('..')).toHaveClass(/text-warn/)
  })
})
