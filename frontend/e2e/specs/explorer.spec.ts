import { readFile } from 'node:fs/promises'

import { AxeBuilder } from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

const themes = [
  'sentinel-dark',
  'sentinel-light',
  'monokai',
  'dracula',
  'nord',
  'one-dark',
  'tokyo-night',
  'gruvbox-dark',
  'solarized-dark',
  'github-dark',
  'solarized-light',
  'github-light',
]

/** Opens the quick connection that the global setup made to the SFTP server of the test stack, at its root. */
async function openQuickConnection(page: Page, quickId: string): Promise<void> {
  await page.goto(`/sites/${quickId}?path=%2F`)
  await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()
}

/**
 * The file explorer against a real SFTP server.
 *
 * Scenarios:
 * - quick connect from its dialog reports success, offers to keep the server and lists it in the tree
 * - the root lists the server's folders, a folder opens with a double click and the breadcrumb and address follow
 * - going up returns to the root
 * - a folder is created, renamed and deleted on the server with the toolbar, F2, Delete and the context menu
 * - a file is uploaded to the server, appears in the folder and in the transfers dock as done, downloads back with the same
 *   content, and is deleted
 * - the explorer has no WCAG violations in any of the twelve themes
 */
test.describe('explorer', () => {
  test('connects from the quick connect dialog', async ({ shell }) => {
    const { page } = shell

    await page.getByRole('button', { name: /Quick connect/ }).click()
    const dialog = page.getByRole('dialog', { name: 'Quick connect' })
    await dialog.getByLabel('Host').fill('sftp')
    await dialog.getByLabel('Username').fill('sentinel')
    await dialog.getByLabel('Password').fill('sentinel')
    await dialog.getByRole('button', { name: 'Connect' }).click()

    await expect(page.getByRole('status')).toContainText('It works. Keep this server?')
    await page.getByRole('button', { name: 'Not now' }).click()
    await page.getByRole('treeitem', { name: /sftp/ }).last().click()
    await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()
  })

  test('browses a real SFTP server', async ({ shell, quickId }) => {
    const { page } = shell

    await openQuickConnection(page, quickId)

    const grid = page.getByRole('grid', { name: 'Files' })
    await expect(grid.getByRole('row', { name: /upload/ })).toBeVisible()
    await expect(page).toHaveURL(/\/sites\/[^?]+\?path=%2F$/)

    await grid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(page).toHaveURL(/path=%2Fupload$/)
    await expect(page.getByRole('button', { name: 'upload', exact: true })).toHaveAttribute('aria-current', 'page')

    await page.getByRole('button', { name: 'Up one level' }).click()
    await expect(page).toHaveURL(/path=%2F$/)
    await expect(grid.getByRole('row', { name: /upload/ })).toBeVisible()
  })

  test('creates, renames and deletes a folder on the server', async ({ shell, quickId }) => {
    const { page } = shell
    const created = `e2e-${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const renamed = `${created}-renamed`
    await openQuickConnection(page, quickId)
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(page).toHaveURL(/path=%2Fupload$/)

    await page.getByRole('button', { name: 'New folder' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(created)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    const row = grid.getByRole('row', { name: new RegExp(created) })
    await expect(row).toBeVisible()

    await row.click()
    await grid.press('F2')
    await page.getByRole('dialog').getByLabel('New name').fill(renamed)
    await page.getByRole('dialog').getByRole('button', { name: 'Rename' }).click()
    const renamedRow = grid.getByRole('row', { name: new RegExp(renamed) })
    await expect(renamedRow).toBeVisible()
    await expect(page.getByRole('complementary', { name: 'Details' })).toContainText(`/upload/${renamed}`)

    const rowBox = (await renamedRow.boundingBox())!
    await renamedRow.click({ button: 'right' })
    const menu = page.getByRole('menu')
    await expect(menu).toBeVisible()
    const box = (await menu.boundingBox())!
    expect(box.y).toBeGreaterThan(rowBox.y - 8)
    expect(box.y).toBeLessThan(rowBox.y + rowBox.height + 8)
    await page.keyboard.press('Escape')

    await grid.press('Delete')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click()
    await expect(renamedRow).toBeHidden()
  })

  test('uploads, downloads and deletes a file on the server', async ({ shell, quickId }) => {
    const { page } = shell
    const name = `e2e-${Date.now()}-${Math.floor(Math.random() * 1e6)}.txt`
    const content = 'hello from the browser test\n'
    await openQuickConnection(page, quickId)
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(page).toHaveURL(/path=%2Fupload$/)

    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from(content) })
    const row = grid.getByRole('row', { name: new RegExp(name) })
    await expect(row).toBeVisible()

    const dock = page.getByRole('region', { name: 'Transfers' })
    await dock.getByRole('tab', { name: /Finished/ }).click()
    const transfer = dock.getByRole('listitem').filter({ hasText: name })
    await expect(transfer).toContainText('Done')
    await expect(transfer).toContainText(`${content.length} B of ${content.length} B`)

    await row.click()
    const download = page.waitForEvent('download')
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()
    const downloaded = await download
    expect(downloaded.suggestedFilename()).toBe(name)
    const path = await downloaded.path()
    expect(await readFile(path, 'utf8')).toBe(content)

    await grid.press('Delete')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click()
    await expect(row).toBeHidden()
  })

  test('has no WCAG violations in any theme', async ({ shell, quickId }) => {
    test.setTimeout(120_000)
    const { page } = shell
    await openQuickConnection(page, quickId)

    for (const theme of themes) {
      await shell.setTheme(theme)
      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
      expect(results.violations, theme).toEqual([])
    }
  })
})
