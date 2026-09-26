import { randomUUID } from 'node:crypto'
import http from 'node:http'

import type { APIRequestContext } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Address of the application under test. */
const baseURL = process.env.SENTINEL_E2E_URL as string

/** An upload the test keeps open by hand, so it holds a slot of the site until it is ended. */
interface HeldUpload {
  /** Identifier of the transfer it creates. */
  id: string
  /** Sends the rest of the body and lets the server finish. */
  end: () => Promise<number>
}

/**
 * Starts an upload to a site whose body is never finished until asked, as a browser tab that keeps uploading would.
 *
 * The request is made by hand because the browser gives a test no control over the pace of a body.
 */
function holdUpload(cookie: string, csrf: string, files: string, path: string): HeldUpload {
  const id = randomUUID()
  let status: (code: number) => void = () => undefined
  const finished = new Promise<number>((resolve) => (status = resolve))
  const query = new URLSearchParams({ path, transferId: id, mode: 'overwrite' })
  const req = http.request(`${baseURL}${files}/content?${query.toString()}`, {
    method: 'PUT',
    headers: { Cookie: cookie, 'X-CSRF-Token': csrf, 'Content-Type': 'application/octet-stream', 'Transfer-Encoding': 'chunked' },
  })
  req.on('response', (response) => {
    response.resume()
    status(response.statusCode ?? 0)
  })
  req.on('error', () => status(0))
  req.write('holding a slot')
  return { id, end: () => (req.end(), finished) }
}

/** Waits until the server reports a transfer as running. */
async function untilRunning(api: APIRequestContext, id: string): Promise<void> {
  await expect
    .poll(async () => ((await (await api.get(`/api/v1/transfers/${id}`)).json()) as { status?: string }).status, { timeout: 15_000 })
    .toBe('running')
}

/**
 * The transfer queue against the isolated test stack, which lets two transfers per site run at once.
 *
 * Scenarios:
 * - with both slots of a saved site held by uploads, a third upload from the browser waits and says so in the dock
 * - a waiting upload can be given a high priority and paused; it stays paused when a slot frees, and runs and finishes
 *   once it is continued
 */
test.describe('transfer queue', () => {
  test('a third upload waits for a slot, can be paused and continues when released', async ({ shell, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
    const workspaces = (await (await request.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
    const workspace = workspaces[0].id
    const headers = { 'X-CSRF-Token': me.csrfToken }
    const created = await request.post(`/api/v1/workspaces/${workspace}/sites`, {
      headers,
      data: { name: `queue-${Date.now()}`, protocol: 'sftp', host: 'sftp', port: 22, username: 'sentinel', authMethod: 'password', password: 'sentinel' },
    })
    expect(created.ok()).toBe(true)
    const { id: siteId } = (await created.json()) as { id: string }
    const files = `/api/v1/workspaces/${workspace}/sites/${siteId}/files`
    const session = (await request.storageState()).cookies.find((cookie) => cookie.name === 'sentinel_session')
    const cookie = `sentinel_session=${session?.value}`
    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`

    const first = holdUpload(cookie, me.csrfToken, files, `/upload/held-a-${stamp}`)
    const second = holdUpload(cookie, me.csrfToken, files, `/upload/held-b-${stamp}`)
    await untilRunning(request, first.id)
    await untilRunning(request, second.id)

    await page.goto(`/sites/${siteId}?path=%2Fupload`)
    const name = `e2e-queued-${stamp}.txt`
    await page.getByLabel('Choose files to upload').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('queued content') })
    const dock = page.getByRole('region', { name: 'Transfers' })
    const waiting = dock.getByRole('listitem').filter({ hasText: name })
    await expect(waiting).toContainText('Waiting for a free slot')

    await waiting.getByRole('combobox', { name: `Priority of ${name}` }).selectOption('High')
    await expect(waiting.getByRole('combobox', { name: `Priority of ${name}` })).toHaveValue('1')
    await waiting.getByRole('button', { name: `Pause ${name}` }).click()
    await expect(waiting).toContainText('Paused')

    expect(await first.end()).toBe(201)
    await page.waitForTimeout(500)
    await expect(waiting).toContainText('Paused')

    await waiting.getByRole('button', { name: `Let ${name} start` }).click()
    await dock.getByRole('tab', { name: /Finished/ }).click()
    await expect(dock.getByRole('listitem').filter({ hasText: name })).toContainText('Done', { timeout: 30_000 })

    expect(await second.end()).toBe(201)
    for (const path of [`/upload/held-a-${stamp}`, `/upload/held-b-${stamp}`, `/upload/${name}`]) {
      await request.delete(files, { params: { path }, headers })
    }
    await request.delete(`/api/v1/workspaces/${workspace}/sites/${siteId}`, { headers })
  })
})
