import { createHash } from 'node:crypto'
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
 * - opening a connection and moving between folders show a connecting state and a loading bar, and never a blank page
 * - a download shows its progress in the transfers dock, which opens on its own, and ends as done
 * - an upload canceled half way leaves a partial file, and resuming it from the dock sends only the rest, so the file that
 *   comes back is identical to the one that went out
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

  test('resumes a canceled upload and the file arrives intact', async ({ shell, quickId }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const name = `e2e-resume-${Date.now()}-${Math.floor(Math.random() * 1e6)}.bin`
    const content = Buffer.alloc(3 * 1024 * 1024)
    for (let index = 0; index < content.length; index++) {
      content[index] = (index * 31 + (index >> 8)) & 0xff
    }
    await openQuickConnection(page, quickId)
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(page).toHaveURL(/path=%2Fupload$/)

    const client = await page.context().newCDPSession(page)
    await client.send('Network.enable')
    await client.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: 300 * 1024 })
    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'application/octet-stream', buffer: content })

    const dock = page.getByRole('region', { name: 'Transfers' })
    const running = dock.getByRole('listitem').filter({ hasText: name })
    await expect.poll(async () => Number(await running.getByRole('progressbar').getAttribute('aria-valuenow'))).toBeGreaterThanOrEqual(10)
    await running.getByRole('button', { name: `Cancel ${name}` }).click()

    await dock.getByRole('tab', { name: /Finished/ }).click()
    const stopped = dock.getByRole('listitem').filter({ hasText: name })
    await expect(stopped).toContainText('Canceled')
    await expect(stopped.getByRole('button', { name: `Resume ${name}` })).toBeVisible()

    await client.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 })
    const resumed = page.waitForRequest((request) => request.method() === 'PUT' && request.url().includes('/files/content'))
    const finished = page.waitForResponse((response) => response.request().method() === 'PUT' && response.url().includes('/files/content'))
    await stopped.getByRole('button', { name: `Resume ${name}` }).click()
    const url = new URL((await resumed).url())
    expect(url.searchParams.get('mode')).toBe('resume')
    expect(Number(url.searchParams.get('offset'))).toBeGreaterThan(0)
    expect(Number(url.searchParams.get('offset'))).toBeLessThan(content.length)
    expect(url.searchParams.get('size')).toBe(String(content.length))
    await finished
    await dock.getByRole('tab', { name: /Finished/ }).click()
    await expect(stopped).toContainText('Done', { timeout: 30_000 })
    await expect(stopped).toContainText('3.0 MB of 3.0 MB')

    const row = grid.getByRole('row', { name: new RegExp(name) })
    await row.click()
    const download = page.waitForEvent('download')
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()
    const received = await readFile((await (await download).path()) as string)
    expect(createHash('sha256').update(received).digest('hex')).toBe(createHash('sha256').update(content).digest('hex'))

    await grid.press('Delete')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click()
    await expect(row).toBeHidden()
  })

  test('shows loading states while connecting and moving between folders', async ({ shell, quickId }) => {
    const { page } = shell
    await page.route('**/files?**', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 900))
      await route.continue()
    })

    await page.goto(`/sites/${quickId}?path=%2F`)
    await expect(page.getByRole('status')).toContainText('Connecting to')
    await expect(page.locator('[data-slot="skeleton"]').first()).toBeVisible()
    const grid = page.getByRole('grid', { name: 'Files' })
    await expect(grid).toBeVisible()
    await expect(page.getByRole('status')).toHaveCount(0)

    const bar = page.getByLabel(/^Opening/)
    await grid.getByRole('row', { name: /upload/ }).dblclick()
    await expect(bar).toHaveAttribute('aria-busy', 'true')
    await expect(page.getByRole('button', { name: 'upload', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(grid.getByRole('row', { name: /upload/ })).toBeVisible()
    await expect(bar).toHaveAttribute('aria-busy', 'false')
  })

  test('shows the progress of a download in the transfers dock', async ({ shell, quickId, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const name = `e2e-download-${Date.now()}-${Math.floor(Math.random() * 1e6)}.bin`
    await request.put(`${files}/content`, {
      params: { path: `/upload/${name}`, mode: 'overwrite' },
      data: Buffer.alloc(24 * 1024 * 1024, 7),
      headers: { 'X-CSRF-Token': me.csrfToken, 'Content-Type': 'application/octet-stream' },
    })

    await page.goto(`/sites/${quickId}?path=%2Fupload`)
    const row = page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: new RegExp(name) })
    await row.click()
    await expect(async () => {
      if (await page.locator('#queue-body').isVisible()) {
        await page.getByRole('button', { name: 'Hide transfers' }).click()
      }
      await expect(page.locator('#queue-body')).toBeHidden({ timeout: 1000 })
    }).toPass()
    const client = await page.context().newCDPSession(page)
    await client.send('Network.enable')
    await client.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: 400 * 1024, uploadThroughput: -1 })

    const download = page.waitForEvent('download')
    await page.getByRole('complementary', { name: 'Details' }).getByRole('button', { name: 'Download' }).click()

    const dock = page.getByRole('region', { name: 'Transfers' })
    const active = dock.getByRole('listitem').filter({ hasText: name }).filter({ has: page.getByRole('img', { name: 'Download' }) })
    await expect(dock.getByRole('tab', { name: /Active/ })).toHaveAttribute('aria-selected', 'true')
    await expect(active.getByRole('img', { name: 'Download' })).toBeVisible()
    await expect.poll(async () => Number(await active.getByRole('progressbar').getAttribute('aria-valuenow'))).toBeGreaterThanOrEqual(5)
    await expect(active).toContainText('/s')

    await client.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 })
    await (await download).path()
    await dock.getByRole('tab', { name: /Finished/ }).click()
    await expect(dock.getByRole('listitem').filter({ hasText: name }).filter({ has: page.getByRole('img', { name: 'Download' }) })).toContainText('Done', { timeout: 30_000 })

    await request.delete(files, { params: { path: `/upload/${name}` }, headers: { 'X-CSRF-Token': me.csrfToken } })
  })

  test('has no WCAG violations in any theme', async ({ shell, quickId }) => {
    test.setTimeout(120_000)
    const { page } = shell
    await openQuickConnection(page, quickId)

    for (const theme of themes) {
      await shell.setTheme(theme)
      await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()
      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
      expect(results.violations, theme).toEqual([])
    }
  })
})
