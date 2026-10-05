import { stat } from 'node:fs/promises'

import { expect, test } from '../fixtures.ts'

/**
 * An SFTP server that reports every file 300 bytes larger than it is (strict-sftp), as the enterprise gateways that motivated the
 * handling of strict servers do (docs/guides/pkg-sftp.md).
 *
 * Scenarios:
 * - a file of 1.1 MB downloads in the browser whole, with its real bytes, instead of hanging while the browser waits for bytes that
 *   never come
 * - the viewer lists the entries of a ZIP of 1 MB and shows one of them
 */
test.describe('strict sizes', () => {
  test('downloads and previews large files of a server whose sizes do not hold', async ({ shell }) => {
    test.setTimeout(90_000)
    const { page } = shell
    const name = `Strict ${Date.now()}`
    await page.getByRole('button', { name: 'New site' }).first().click()
    const dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByRole('button', { name: /^SFTP/ }).click()
    await dialog.getByLabel('Name', { exact: true }).fill(name)
    await dialog.getByLabel('Host', { exact: true }).fill('strict-sftp')
    await dialog.getByLabel('Username', { exact: true }).fill('transferly')
    await dialog.getByLabel('Password', { exact: true }).fill('transferly')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()
    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const trust = page.getByRole('button', { name: 'Trust and connect' })
    const grid = page.getByRole('grid', { name: 'Files' })
    await expect(trust.or(grid)).toBeVisible()
    if (await trust.isVisible()) {
      await trust.click()
    }
    const file = grid.getByRole('row', { name: /export\.dat/ })
    await expect(file).toBeVisible()

    const downloading = page.waitForEvent('download')
    await file.dblclick()
    const download = await downloading
    expect(await download.failure()).toBeNull()
    expect((await stat(await download.path())).size).toBe(1_100_000)

    await grid.getByRole('row', { name: /bundle\.zip/ }).click()
    await page.keyboard.press(' ')
    const viewer = page.getByRole('dialog', { name: 'bundle.zip' })
    await expect(viewer.getByText('data.bin')).toBeVisible({ timeout: 20_000 })
    await expect(viewer.getByText('readme.txt')).toBeVisible()
  })
})
