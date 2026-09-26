import { expect, test } from '../fixtures.ts'

/**
 * The activity log of the isolated test stack.
 *
 * Scenarios:
 * - a folder created and a file checksummed on the SFTP server appear in the activity tab of the transfers dock with their path, and a
 *   failed delete shows its error code
 */
test.describe('activity log', () => {
  test('lists what was done on a connection', async ({ shell, quickId, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const folder = `e2e-activity-${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${folder}` } })).ok()).toBe(true)
    expect((await request.delete(files, { params: { path: `/upload/${folder}/missing` }, headers })).status()).toBe(404)

    await page.goto('/')
    const dock = page.getByRole('region', { name: 'Transfers' })
    await dock.getByRole('tab', { name: 'Activity' }).click()
    const log = dock.getByRole('list', { name: 'Activity log' })

    const created = log.getByRole('listitem').filter({ hasText: `/upload/${folder}` }).filter({ hasText: 'Created a folder' })
    await expect(created).toBeVisible()
    await expect(log.getByRole('listitem').filter({ hasText: `/upload/${folder}/missing` })).toContainText('path_not_found')

    await request.delete(files, { params: { path: `/upload/${folder}`, recursive: true }, headers })
  })
})
