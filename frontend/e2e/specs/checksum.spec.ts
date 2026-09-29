import { createHash } from 'node:crypto'

import { expect, test } from '../fixtures.ts'
import { choose } from '../select.ts'

/**
 * Checksums of files on the SFTP server of the isolated test stack.
 *
 * Scenarios:
 * - the inspector computes the SHA-256 and the MD5 of a file, and they match the digests of its content computed here
 */
test.describe('checksums', () => {
  test('computes the checksum of a file from the inspector', async ({ shell, quickId, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const files = `/api/v1/workspaces/${workspaces[0].id}/sites/${quickId}/files`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
    const parent = `e2e-sum-${stamp}`
    const content = Buffer.from(`content to digest ${stamp}\n`.repeat(2000))
    expect((await request.post(`${files}/directories`, { headers, data: { path: `/upload/${parent}` } })).ok()).toBe(true)
    expect(
      (
        await request.put(`${files}/content`, {
          params: { path: `/upload/${parent}/sum.txt`, mode: 'overwrite' },
          data: content,
          headers: { ...headers, 'Content-Type': 'application/octet-stream' },
        })
      ).ok(),
    ).toBe(true)

    await page.goto(`/sites/${quickId}?path=%2Fupload%2F${parent}`)
    await page.getByRole('grid', { name: 'Files' }).getByRole('row', { name: /sum.txt/ }).click()
    const inspector = page.getByRole('complementary', { name: 'Details' })

    await inspector.getByRole('button', { name: 'Checksum' }).click()
    await expect(inspector.getByLabel('Checksum value')).toHaveText(createHash('sha256').update(content).digest('hex'))
    await choose(inspector.getByRole('combobox', { name: 'Checksum algorithm' }), 'MD5')
    await inspector.getByRole('button', { name: 'Checksum' }).click()
    await expect(inspector.getByLabel('Checksum value')).toHaveText(createHash('md5').update(content).digest('hex'))

    await request.delete(files, { params: { path: `/upload/${parent}`, recursive: true }, headers })
  })
})
