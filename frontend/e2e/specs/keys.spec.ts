import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/**
 * Private keys given as files (4.8g), against a real OpenSSH server that authorizes the test PuTTY key.
 *
 * Scenarios:
 * - a public key chosen by mistake is recognized and the user is told to pick the private one
 * - a PuTTY key file is described by the server (format, type, size, fingerprint), asks for its passphrase, says when it is ready, and
 *   the site saved with it connects and lists the folder
 */
test.describe('key files', () => {
  test('saves a site with a PuTTY key file and connects with it', async ({ shell }) => {
    const { page } = shell
    const name = `PuTTY key ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await choose(dialog.getByLabel('Logon type'), 'Key file')
    await dialog.getByLabel('Username', { exact: true }).fill('sentinel')

    await dialog.getByLabel('Key file', { exact: true }).setInputFiles('e2e/keys/sentinel.pub')
    await expect(dialog.getByRole('alert')).toContainText('This is a public key')
    await dialog.getByRole('button', { name: 'Remove the key file' }).click()

    await dialog.getByLabel('Key file', { exact: true }).setInputFiles('e2e/keys/sentinel.ppk')
    await expect(dialog.getByText('Enter the passphrase of this key.')).toBeVisible()
    await expect(dialog.getByText(/PuTTY PPK 3 · private key · ssh-rsa · 2048 bits/)).toBeVisible()
    await expect(dialog.getByText(/^SHA256:/)).toBeVisible()
    await dialog.getByLabel('Key passphrase').fill('correct horse')
    await expect(dialog.getByText('Ready to use.')).toBeVisible()
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()
  })
})
