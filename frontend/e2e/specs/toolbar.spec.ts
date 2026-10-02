import { expect, test } from '../fixtures.ts'

/**
 * The toolbar (4.9b) against the SFTP server of the isolated stack.
 *
 * Scenarios:
 * - refresh lists the folder in view again, and the log tells it
 * - disconnect closes the connection, leaves the site so a reload does not connect it again, and opening the site from the
 *   sidebar connects again at the root, not in the folder the user had opened (4.14i)
 * - a site with a default remote folder opens there, and again there after a disconnect
 */
test.describe('toolbar', () => {
  test('refreshes, disconnects and connects again', async ({ shell }) => {
    const { page } = shell
    const name = `Toolbar ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await dialog.getByLabel('Username', { exact: true }).fill('transferly')
    await dialog.getByLabel('Password', { exact: true }).fill('transferly')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const toolbar = page.getByRole('toolbar', { name: 'Toolbar' })
    const log = page.getByRole('list', { name: 'Connection log' })
    await expect(log.getByText(/^Listed \//)).toHaveCount(1)
    await expect(toolbar.getByText(`Connection: ${name}`)).toBeAttached()

    await toolbar.getByRole('button', { name: 'Refresh the folder' }).click()
    await expect(log.getByText(/^Listed \//)).toHaveCount(2)
    await page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ }).dblclick()
    await expect(page).toHaveURL(/path=%2Fupload/)

    await toolbar.getByRole('button', { name: 'Disconnect from the server' }).click()
    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('grid', { name: 'Files' })).toHaveCount(0)
    await page.reload()
    await expect(page.getByRole('grid', { name: 'Files' })).toHaveCount(0)

    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    await expect(page).toHaveURL(/path=%2F$/)
    await expect(page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /upload/ })).toBeVisible()
  })

  test('opens a site in its default remote folder', async ({ shell }) => {
    const { page } = shell
    const name = `Starts in upload ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByLabel('Host', { exact: true }).fill('sftp')
    await dialog.getByLabel('Username', { exact: true }).fill('transferly')
    await dialog.getByLabel('Password', { exact: true }).fill('transferly')
    await dialog.getByRole('tab', { name: 'Advanced' }).click()
    await dialog.getByLabel('Default remote folder').fill('/upload')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    const row = page.getByRole('treeitem', { name: new RegExp(name) })
    await row.dblclick()
    await expect(page).toHaveURL(/path=%2Fupload/)
    await page.getByRole('button', { name: 'Up one level' }).click()
    await expect(page).toHaveURL(/path=%2F$/)
    await page.getByRole('toolbar', { name: 'Toolbar' }).getByRole('button', { name: 'Disconnect from the server' }).click()
    await expect(page).toHaveURL(/\/$/)
    await row.dblclick()
    await expect(page).toHaveURL(/path=%2Fupload/)
  })
})
