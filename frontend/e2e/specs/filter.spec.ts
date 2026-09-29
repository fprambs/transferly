import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/**
 * The instant filter of a folder (4.14d) against the SFTP server of the isolated stack.
 *
 * Scenarios:
 * - typing filters the folder without listing it again, highlights the words and says how many entries it keeps; a wildcard pattern
 *   and a type from the filters menu narrow it; clearing brings everything back
 * - / focuses the box and Escape clears it
 * - the month and year choices of the calendar open inside the filters menu and move it to the month chosen
 */
test.describe('folder filter', () => {
  test('filters the folder in view as the user types', async ({ shell, quickId, folder }) => {
    const { page } = shell
    const me = (await (await page.request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspace = ((await (await page.request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>)[0].id
    const files = `/api/v1/workspaces/${workspace}/sites/${quickId}/files`
    for (const name of ['access.log', 'error.log', 'photo.png']) {
      await page.request.put(`${files}/content`, { params: { path: `${folder}/${name}`, mode: 'overwrite' }, data: Buffer.from('x'), headers: { 'X-CSRF-Token': me.csrfToken, 'Content-Type': 'application/octet-stream' } })
    }
    await page.request.post(`${files}/directories`, { headers: { 'X-CSRF-Token': me.csrfToken }, data: { path: `${folder}/logs` } })
    await page.goto(`/sites/${quickId}?path=${encodeURIComponent(folder)}`)
    const grid = page.getByRole('grid', { name: 'Files' })
    await expect(grid.getByRole('row')).toHaveCount(5)
    let listings = 0
    page.on('request', (request) => request.url().includes('/files?') && listings++)

    await page.keyboard.press('/')
    const box = page.getByRole('searchbox', { name: 'Filter this folder' })
    await expect(box).toBeFocused()
    await box.pressSequentially('log')
    await expect(grid.getByRole('row')).toHaveCount(4)
    await expect(grid.locator('mark', { hasText: 'log' })).toHaveCount(3)
    await expect(page.getByText('3 of 4')).toBeVisible()

    await box.fill('*.log')
    await expect(grid.getByRole('row')).toHaveCount(3)
    await page.getByRole('button', { name: /Filters, 0 in use/ }).click()
    const menu = page.getByRole('dialog')
    await menu.getByRole('button', { name: 'Between dates' }).click()
    await choose(menu.getByRole('combobox', { name: 'Choose the Year' }), '2020')
    await expect(menu.getByRole('combobox', { name: 'Choose the Year' })).toHaveText('2020')
    await choose(menu.getByRole('combobox', { name: 'Choose the Month' }), /june/i)
    await expect(menu.getByRole('button', { name: /June 1st, 2020/ })).toBeVisible()
    await menu.getByRole('button', { name: 'Any time' }).click()
    await menu.getByRole('button', { name: 'Images' }).click()
    await expect(page.getByText('Nothing matches the filter')).toBeVisible()
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Clear filters' }).last().click()
    await expect(grid.getByRole('row')).toHaveCount(5)
    expect(listings).toBe(0)
  })
})
