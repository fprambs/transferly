import type { Locator } from '@playwright/test'

import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/** Reads the computed background color of an element in the page. */
function background(locator: Locator): Promise<string> {
  return locator.evaluate((element) => element.ownerDocument.defaultView!.getComputedStyle(element).backgroundColor)
}

/** Builds a name that no other run or test has used, since every test shares one database. */
function unique(prefix: string): string {
  return `${prefix} ${Date.now()}-${Math.floor(Math.random() * 1e6)}`
}

/**
 * Collection and site management in the sidebar.
 *
 * Scenarios:
 * - a collection is created from the sidebar and appears in the tree
 * - the actions menu of a row opens next to its button and stays there when the pointer leaves the row
 * - the menu opens below its button, or above it when there is no room, and the dialogs it opens are centered in the window
 * - rows show a pointer, a visible hover shade and a selected state, and menu items highlight under the pointer
 * - a site is created inside a collection with a speed limit, a proxy and a jump host, all of which are kept, and edited from its menu
 * - a site is dragged into a collection and back to the top level of the tree, and the server keeps each move (4.14h)
 */
test.describe('sites sidebar', () => {
  test('creates a collection', async ({ shell }) => {
    const { page } = shell
    const name = unique('Collection')

    await page.getByRole('button', { name: 'New collection' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(name)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()

    await expect(page.getByRole('treeitem', { name: new RegExp(name) })).toBeVisible()
  })

  test('keeps the actions menu next to its button when the pointer moves away', async ({ shell }) => {
    const { page } = shell
    const name = unique('Menu')
    await page.getByRole('button', { name: 'New collection' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(name)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    const row = page.getByRole('treeitem', { name: new RegExp(name) })
    await row.hover()

    const trigger = page.getByRole('button', { name: `Actions for ${name}` })
    await trigger.click()
    const menu = page.getByRole('menu')
    await expect(menu).toBeVisible()
    await page.waitForTimeout(400)
    const before = await menu.boundingBox()
    const anchor = await trigger.boundingBox()

    await page.mouse.move(900, 500)
    await page.waitForTimeout(400)

    await expect(menu).toBeVisible()
    await expect(trigger).toBeVisible()
    const after = await menu.boundingBox()
    expect(before).not.toBeNull()
    expect(after).toEqual(before)
    expect(Math.abs(after!.x + after!.width - (anchor!.x + anchor!.width))).toBeLessThan(8)
    const gapBelow = after!.y - (anchor!.y + anchor!.height)
    const gapAbove = anchor!.y - (after!.y + after!.height)
    expect(Math.max(gapBelow, gapAbove)).toBeGreaterThanOrEqual(0)
    expect(Math.max(gapBelow, gapAbove)).toBeLessThan(16)
  })

  test('shows hover, pointer and selection feedback on rows and menu items', async ({ shell }) => {
    const { page } = shell
    const name = unique('Feedback')
    await page.getByRole('button', { name: 'New collection' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(name)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    const row = page.getByRole('treeitem', { name: new RegExp(name) })
    await expect(row).toBeVisible()

    await page.mouse.move(900, 500)
    const resting = await background(row)
    await row.hover()
    await expect(row).toHaveCSS('cursor', 'pointer')
    await expect.poll(() => background(row)).not.toBe(resting)

    await row.click()
    await expect(row).toHaveAttribute('aria-selected', 'true')
    expect(await background(row)).not.toBe('rgba(0, 0, 0, 0)')

    await page.getByRole('button', { name: `Actions for ${name}` }).click()
    const item = page.getByRole('menuitem', { name: 'Edit' })
    const popup = page.getByRole('menu')
    const idle = await background(item)
    const surface = await background(popup)
    await item.hover()
    await expect(item).toHaveCSS('cursor', 'pointer')
    await expect.poll(() => background(item)).not.toBe(surface)
    expect(idle).not.toBe('')
  })

  test('creates a site in a collection and edits it, with centered dialogs', async ({ shell }) => {
    const { page } = shell
    const collection = unique('Group')
    const site = unique('Site')
    await page.getByRole('button', { name: 'New collection' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill(collection)
    await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
    await page.getByRole('treeitem', { name: new RegExp(collection) }).hover()
    await page.getByRole('button', { name: `Actions for ${collection}` }).click()
    await page.getByRole('menuitem', { name: 'New site here' }).click()

    const dialog = page.getByRole('dialog', { name: 'New site' })
    await expect(dialog).toBeVisible()
    const viewport = page.viewportSize()!
    const box = (await dialog.boundingBox())!
    expect(Math.abs(box.x + box.width / 2 - viewport.width / 2)).toBeLessThan(4)

    await dialog.getByLabel('Name', { exact: true }).fill(site)
    await dialog.getByLabel('Host', { exact: true }).fill('8.8.8.8')
    await dialog.getByLabel('Username', { exact: true }).fill('deploy')
    await dialog.getByLabel('Password', { exact: true }).fill('secret')
    await dialog.getByRole('tab', { name: 'Transfer' }).click()
    await dialog.getByLabel('Speed limit (MiB/s)').fill('4')
    await dialog.getByRole('tab', { name: 'Proxy and jump host' }).click()
    await choose(dialog.getByLabel('Proxy', { exact: true }), 'SOCKS5')
    await dialog.getByLabel('Proxy host').fill('9.9.9.9')
    await dialog.getByLabel('Jump host', { exact: true }).fill('1.1.1.1')
    await dialog.getByLabel('Jump username').fill('bastion')
    await dialog.getByLabel('Jump host password').fill('bastionpass')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()

    const siteRow = page.getByRole('treeitem', { name: new RegExp(site) })
    await siteRow.hover()
    await page.getByRole('button', { name: `Actions for ${site}` }).click()
    await page.mouse.move(900, 500)
    await page.waitForTimeout(300)
    await page.getByRole('menuitem', { name: 'Edit' }).click()

    const edit = page.getByRole('dialog', { name: 'Edit site' })
    await expect(edit).toBeVisible()
    await expect(edit.getByLabel('Host', { exact: true })).toHaveValue('8.8.8.8')
    await edit.getByRole('tab', { name: 'Transfer' }).click()
    await expect(edit.getByLabel('Speed limit (MiB/s)')).toHaveValue('4')
    await edit.getByRole('tab', { name: 'Proxy and jump host' }).click()
    await expect(edit.getByLabel('Proxy', { exact: true })).toHaveText('SOCKS5')
    await expect(edit.getByLabel('Proxy host')).toHaveValue('9.9.9.9')
    await expect(edit.getByLabel('Jump host', { exact: true })).toHaveValue('1.1.1.1')
    await expect(edit.getByLabel('Jump username')).toHaveValue('bastion')
    const editBox = (await edit.boundingBox())!
    expect(Math.abs(editBox.x + editBox.width / 2 - viewport.width / 2)).toBeLessThan(4)
  })

  test('moves a site between collections by dragging it', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspace = ((await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>)[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const collection = unique('Drop here')
    const name = unique('Dragged')
    const made = (await (await request.post(`/api/v1/workspaces/${workspace}/collections`, { headers, data: { name: collection } })).json()) as { id: string }
    const created = (await (await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name, protocol: 'sftp', host: 'sftp', port: 22, username: 'transferly', authMethod: 'password', password: 'transferly' },
    })).json()) as { id: string }
    await page.reload()
    const siteRow = page.getByRole('treeitem', { name: new RegExp(name) })
    const collectionRow = page.getByRole('treeitem', { name: new RegExp(collection) })

    await siteRow.dragTo(collectionRow)
    await expect(collectionRow).toHaveAttribute('aria-expanded', 'true')
    await expect(siteRow).toHaveAttribute('aria-level', '2')
    const inside = (await (await request.get(`/api/v1/workspaces/${workspace}/sites/${created.id}`)).json()) as { collectionId?: string }
    expect(inside.collectionId).toBe(made.id)

    await siteRow.dragTo(page.getByRole('tree', { name: 'Sites' }), { targetPosition: { x: 20, y: 5 } })
    await expect(siteRow).toHaveAttribute('aria-level', '1')
  })
})
