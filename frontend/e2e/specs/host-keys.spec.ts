import type { APIRequestContext } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Name of the SFTP server of the test stack whose trusted keys only this test touches. */
const host = 'sftp-rotating'

/** A well formed fingerprint that is not the key of the server. */
const stranger = 'SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU'

/** Learns the key the server presents from a listing; when an earlier run left it trusted, it is forgotten and the connection closed. */
async function prepare(request: APIRequestContext, siteId: string, workspace: string, headers: Record<string, string>): Promise<{ keyType: string; fingerprint: string }> {
  const listed = await request.get(`/api/v1/workspaces/${workspace}/sites/${siteId}/files`, { params: { path: '/' } })
  const body = (await listed.json()) as { code: string; params: { keyType: string; fingerprint: string } }
  if (body.code === 'host_key_unknown' || body.code === 'host_key_changed') {
    return body.params
  }
  const keys = (await (await request.get(`/api/v1/workspaces/${workspace}/known-hosts`)).json()) as Array<{ host: string; keyType: string; fingerprint: string }>
  const known = keys.find((key) => key.host === host) as { keyType: string; fingerprint: string }
  await request.delete(`/api/v1/workspaces/${workspace}/known-hosts`, { headers, params: { host, port: '22', keyType: known.keyType } })
  await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}/connection`, { headers })
  return known
}

/**
 * Servers that present a key other than the trusted one (4.13b), against the SFTP server of the isolated stack under a name of its own.
 *
 * Scenarios:
 * - with another key trusted for the server, opening the site warns that the key is not the one trusted, shows both fingerprints and offers
 *   no silent way in; trusting the key too connects and keeps both keys, each listed and forgotten on its own on the trusted servers page
 * - replacing the trusted keys connects and leaves only the key of the server
 */
test.describe('changed host keys', () => {
  test('adds or replaces a trusted key after a warning', async ({ shell, request }) => {
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspace = ((await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>)[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const name = `Rotating ${Date.now()}`
    const created = await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name, connector: 'sftp', config: { host, port: '22', username: 'transferly', auth: 'password' }, secrets: { password: 'transferly' } },
    })
    expect(created.ok()).toBe(true)
    const { id: siteId } = (await created.json()) as { id: string }
    const real = await prepare(request, siteId, workspace, headers)
    const trustStranger = async (): Promise<void> => {
      await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}/connection`, { headers })
      await request.delete(`/api/v1/workspaces/${workspace}/known-hosts`, { headers, params: { host, port: '22', keyType: real.keyType } })
      expect((await request.post(`/api/v1/workspaces/${workspace}/known-hosts`, { headers, data: { host, port: 22, keyType: real.keyType, fingerprint: stranger } })).ok()).toBe(true)
    }
    const trusted = async (): Promise<string[]> => {
      const keys = (await (await request.get(`/api/v1/workspaces/${workspace}/known-hosts`)).json()) as Array<{ host: string; fingerprint: string }>
      return keys.filter((key) => key.host === host).map((key) => key.fingerprint).sort()
    }
    const grid = page.getByRole('grid', { name: 'Files' })

    await trustStranger()
    await page.reload()
    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    const warning = page.getByRole('alert').filter({ hasText: 'The server key is not the one you trusted' })
    await expect(warning).toBeVisible()
    await expect(warning.getByText(stranger)).toBeVisible()
    await expect(warning.getByText(real.fingerprint)).toBeVisible()
    await expect(warning.getByRole('button', { name: 'Trust and connect' })).toHaveCount(0)
    await warning.getByRole('button', { name: 'Trust this key too' }).click()
    await expect(grid.getByRole('row', { name: /upload/ })).toBeVisible()
    expect(await trusted()).toEqual([real.fingerprint, stranger].sort())

    await page.goto('/servers')
    await page.getByRole('button', { name: `Forget ${host}:22 ${stranger}` }).click()
    await page.getByRole('dialog', { name: 'Forget this key?' }).getByRole('button', { name: 'Forget' }).click()
    await expect(page.getByRole('button', { name: `Forget ${host}:22 ${stranger}` })).toHaveCount(0)
    await expect(page.getByRole('button', { name: `Forget ${host}:22 ${real.fingerprint}` })).toBeVisible()

    await trustStranger()
    await page.goto('/')
    await page.getByRole('treeitem', { name: new RegExp(name) }).dblclick()
    await page.getByRole('button', { name: 'Replace the trusted keys' }).click()
    await expect(grid.getByRole('row', { name: /upload/ })).toBeVisible()
    expect(await trusted()).toEqual([real.fingerprint])

    await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}`, { headers })
  })
})
