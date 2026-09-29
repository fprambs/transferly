import type { Locator } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Reads the computed background color of an element. */
function background(locator: Locator): Promise<string> {
  return locator.evaluate((element) => element.ownerDocument.defaultView!.getComputedStyle(element).backgroundColor)
}

/**
 * What the pointer tells about the controls of the interface, against the SFTP server of the isolated stack.
 *
 * Scenarios:
 * - every enabled button, tab, menu item and tree row in view shows the hand
 * - the buttons of the explorer toolbar and of the main toolbar change color under the pointer
 */
test.describe('interaction', () => {
  test('shows the hand on everything clickable and reacts to the pointer', async ({ shell, quickId }) => {
    const { page } = shell
    await page.goto(`/sites/${quickId}?path=%2F`)
    await expect(page.getByRole('grid', { name: 'Files' })).toBeVisible()

    const arrows = await page
      .locator('button:not(:disabled), [role="tab"], [role="treeitem"], [role="menuitem"], a[href]')
      .evaluateAll((elements) =>
        elements
          .filter((element) => element.getClientRects().length > 0 && element.ownerDocument.defaultView!.getComputedStyle(element).cursor !== 'pointer')
          .map((element) => element.getAttribute('aria-label') ?? element.textContent?.trim() ?? element.tagName),
      )
    expect(arrows, 'controls without the hand').toEqual([])

    for (const button of [page.getByRole('button', { name: 'New folder' }), page.getByRole('button', { name: 'Refresh', exact: true }), page.getByRole('button', { name: 'Refresh the folder' })]) {
      const resting = await background(button)
      await button.hover()
      await expect.poll(() => background(button)).not.toBe(resting)
      await page.mouse.move(0, 0)
    }
  })
})
