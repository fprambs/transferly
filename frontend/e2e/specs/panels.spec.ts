import { expect, test } from '../fixtures.ts'

/**
 * Resizable panels (4.10) in a real browser, where layout is measured.
 *
 * Scenarios:
 * - dragging the separator of the transfers panel makes it smaller, the size survives a reload, and a double click restores the default
 * - the log panel resizes from the keyboard with the arrow keys on its focused separator
 * - hiding the sites panel leaves a rail and gives the files its room
 */
test.describe('resizable panels', () => {
  test('resizes, remembers and restores the transfers panel', async ({ shell, quickId }) => {
    const { page } = shell
    await page.goto(`/sites/${quickId}?path=%2F`)
    await page.getByRole('grid', { name: 'Files' }).waitFor()
    const transfers = page.getByRole('region', { name: 'Transfers' })
    const initial = (await transfers.boundingBox())?.height ?? 0
    const handle = page.getByRole('separator', { name: 'Resize the transfers panel' })
    const box = (await handle.boundingBox()) as { x: number; y: number; width: number; height: number }

    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2, box.y + 80, { steps: 8 })
    await page.mouse.up()
    const smaller = (await transfers.boundingBox())?.height ?? 0
    expect(smaller).toBeLessThan(initial - 40)

    await page.reload()
    await page.getByRole('grid', { name: 'Files' }).waitFor()
    await expect.poll(async () => Math.round((await transfers.boundingBox())?.height ?? 0)).toBe(Math.round(smaller))

    await page.getByRole('separator', { name: 'Resize the transfers panel' }).dblclick()
    await expect.poll(async () => Math.round((await transfers.boundingBox())?.height ?? 0)).toBe(Math.round(initial))
  })

  test('resizes the log from the keyboard', async ({ shell, quickId }) => {
    const { page } = shell
    await page.goto(`/sites/${quickId}?path=%2F`)
    const log = page.getByRole('region', { name: 'Log' })
    await log.waitFor()
    const before = (await log.boundingBox())?.height ?? 0
    const handle = page.getByRole('separator', { name: 'Resize the log panel' })
    await handle.focus()
    for (let i = 0; i < 3; i++) {
      await page.keyboard.press('ArrowUp')
    }
    await expect.poll(async () => (await log.boundingBox())?.height ?? 0).toBeLessThan(before)
  })

  test('hides the sites panel and leaves a rail', async ({ shell, quickId }) => {
    const { page } = shell
    await page.goto(`/sites/${quickId}?path=%2F`)
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.waitFor()
    const width = (await grid.boundingBox())?.width ?? 0

    await page.getByRole('toolbar', { name: 'Toolbar' }).getByRole('button', { name: 'View' }).click()
    await page.getByRole('menuitemcheckbox', { name: 'Sites panel' }).click()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('navigation', { name: 'Sites, panel hidden' })).toBeVisible()
    await expect.poll(async () => (await grid.boundingBox())?.width ?? 0).toBeGreaterThan(width + 100)
    await page.getByRole('navigation', { name: 'Sites, panel hidden' }).getByRole('button', { name: 'Show the sites panel' }).click()
    await expect(page.getByRole('tree', { name: 'Sites' })).toBeVisible()
  })
})
