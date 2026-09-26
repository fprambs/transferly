import http from 'node:http'

import { request as playwrightRequest, type APIRequestContext } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** Address of the application under test. */
const baseURL = process.env.SENTINEL_E2E_URL as string

/** What the tests need to call the API as the signed-in account. */
interface Caller {
  /** The API client, carrying the session cookie. */
  api: APIRequestContext
  /** Anti-CSRF token of the session. */
  csrf: string
  /** Identifier of the personal workspace. */
  workspaceId: string
  /** Value of the session cookie, for raw requests. */
  cookie: string
}

/** Reads the session, the workspace and the cookie the API calls need. */
async function caller(api: APIRequestContext): Promise<Caller> {
  const me = (await (await api.get('/api/v1/me')).json()) as { csrfToken: string }
  const workspaces = (await (await api.get('/api/v1/workspaces')).json()) as Array<{ id: string }>
  const cookies = (await api.storageState()).cookies
  const session = cookies.find((cookie) => cookie.name === 'sentinel_session')
  return { api, csrf: me.csrfToken, workspaceId: workspaces[0].id, cookie: `sentinel_session=${session?.value}` }
}

/** Opens a WebSocket upgrade by hand, since a browser cannot forge its own `Origin`, and reports how the server answered. */
function upgrade(cookie: string, origin: string | undefined): Promise<string> {
  return new Promise((resolve) => {
    const headers: Record<string, string> = {
      Cookie: cookie,
      Upgrade: 'websocket',
      Connection: 'Upgrade',
      'Sec-WebSocket-Version': '13',
      'Sec-WebSocket-Key': 'dGhlIHNhbXBsZSBub25jZQ==',
    }
    if (origin !== undefined) {
      headers.Origin = origin
    }
    const req = http.request(`${baseURL}/api/v1/ws`, { headers })
    req.on('response', (response) => {
      resolve(`status ${response.statusCode}`)
      response.resume()
    })
    req.on('upgrade', (_response, socket) => {
      resolve('upgraded')
      socket.destroy()
    })
    req.on('error', (error) => resolve(`error ${error.message}`))
    req.end()
  })
}

/**
 * Attacks the running application, as regressions of the security review of phase 1.
 *
 * Scenarios:
 * - every response carries the security headers, and the application page and the API are never cached publicly
 * - without a session the API answers 401, with a session but no matching anti-CSRF token changes are 403, and a request
 *   from a foreign origin is refused even with the token
 * - a WebSocket from a foreign origin is refused, one from the application's own origin is accepted
 * - sites cannot point to loopback, link-local, private, metadata or IPv4-mapped destinations
 * - remote paths with line breaks, NUL or absurd length are refused
 * - upload parameters that make no sense (negative size or offset, unknown mode, a transfer identifier that is not a UUID) are refused
 * - unknown accounts and wrong passwords are indistinguishable at sign in
 * - a downloaded file is always an inert attachment, whatever its name and content
 * - a file or folder named as HTML is shown as text everywhere and runs nothing
 */
test.describe('security', () => {
  test('sends security headers and keeps the API out of caches', async ({ request }) => {
    for (const path of ['/', '/api/v1/me', '/healthz']) {
      const headers = (await request.get(path)).headers()
      expect(headers['content-security-policy'], path).toContain("default-src 'self'")
      expect(headers['content-security-policy'], path).toContain("frame-ancestors 'none'")
      expect(headers['x-content-type-options'], path).toBe('nosniff')
      expect(headers['x-frame-options'], path).toBe('DENY')
      expect(headers['referrer-policy'], path).toBe('strict-origin-when-cross-origin')
      expect(headers['server'], path).toBeUndefined()
    }
    expect((await request.get('/api/v1/me')).headers()['cache-control']).toBe('no-store')
  })

  test('refuses anonymous, token-less and cross-site calls', async ({ request }) => {
    const anonymous = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    expect((await anonymous.get('/api/v1/me')).status()).toBe(401)
    expect((await anonymous.get('/api/v1/workspaces')).status()).toBe(401)
    await anonymous.dispose()

    const { csrf, workspaceId } = await caller(request)
    const path = `/api/v1/workspaces/${workspaceId}/collections`
    const noToken = await request.post(path, { data: { name: 'attack' } })
    expect(noToken.status()).toBe(403)
    expect(((await noToken.json()) as { code: string }).code).toBe('csrf_invalid')
    const wrongToken = await request.post(path, { data: { name: 'attack' }, headers: { 'X-CSRF-Token': 'not-the-token' } })
    expect(((await wrongToken.json()) as { code: string }).code).toBe('csrf_invalid')
    const crossSite = await request.post(path, { data: { name: 'attack' }, headers: { 'X-CSRF-Token': csrf, Origin: 'https://evil.example' } })
    expect(crossSite.status()).toBe(403)
    expect(((await crossSite.json()) as { code: string }).code).toBe('cross_site_request')
    const collections = (await (await request.get(path)).json()) as Array<{ name: string }>
    expect(collections.some((collection) => collection.name === 'attack')).toBe(false)
  })

  test('refuses a WebSocket from a foreign origin', async ({ request }) => {
    const { cookie } = await caller(request)

    expect(await upgrade(cookie, 'https://evil.example')).toBe('status 403')
    expect(await upgrade(cookie, baseURL)).toBe('upgraded')
  })

  test('does not let sites point to internal destinations', async ({ request }) => {
    const { csrf, workspaceId } = await caller(request)
    const hosts = ['127.0.0.1', 'localhost', '169.254.169.254', '0.0.0.0', '::1', '::ffff:127.0.0.1', '10.0.0.5', '192.168.1.1', '100.64.0.1', 'metadata.google.internal']

    for (const host of hosts) {
      const response = await request.post(`/api/v1/workspaces/${workspaceId}/sites`, {
        data: { name: 'ssrf', protocol: 'sftp', host, username: 'u' },
        headers: { 'X-CSRF-Token': csrf },
      })
      const code = ((await response.json()) as { code: string }).code
      expect(response.status(), host).toBe(422)
      expect(['destination_blocked', 'invalid_host', 'host_unresolvable'], `${host}: ${code}`).toContain(code)
    }
    const sites = (await (await request.get(`/api/v1/workspaces/${workspaceId}/sites`)).json()) as Array<{ name: string }>
    expect(sites.some((site) => site.name === 'ssrf')).toBe(false)
  })

  test('refuses hostile paths and upload parameters', async ({ request, quickId }) => {
    const { csrf, workspaceId } = await caller(request)
    const files = `/api/v1/workspaces/${workspaceId}/sites/${quickId}/files`
    for (const path of ['/upload/a\r\nb', '/upload/a\u0000b', `/${'a'.repeat(5000)}`, '']) {
      const response = await request.get(files, { params: { path } })
      expect(response.status(), JSON.stringify(path.slice(0, 20))).toBe(422)
      expect(((await response.json()) as { code: string }).code).toBe('invalid_path')
    }

    const put = (query: string) =>
      request.put(`${files}/content?path=%2Fupload%2Fnever.txt&${query}`, {
        data: Buffer.from('x'),
        headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/octet-stream' },
      })
    const cases: Array<[string, number, string]> = [
      ['size=-5', 422, 'invalid_size'],
      ['mode=resume&offset=-1', 422, 'invalid_offset'],
      ['mode=drop', 422, 'invalid_mode'],
      ['transferId=..%2F..%2Fx', 400, 'invalid_request'],
    ]
    for (const [query, status, code] of cases) {
      const response = await put(query)
      expect(response.status(), query).toBe(status)
      expect(((await response.json()) as { code: string }).code, query).toBe(code)
    }
    const listing = (await (await request.get(files, { params: { path: '/upload' } })).json()) as Array<{ name: string }>
    expect(listing.some((entry) => entry.name === 'never.txt')).toBe(false)
  })

  test('does not tell unknown accounts from wrong passwords', async () => {
    const anonymous = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    const attempt = async (email: string) => {
      const response = await anonymous.post('/api/v1/auth/login', { data: { email, password: 'not the password!!' } })
      return { status: response.status(), body: await response.text() }
    }

    const unknown = await attempt('nobody-at-all@example.com')
    const wrong = await attempt('e2e@example.com')

    expect(unknown.status).toBe(401)
    expect(wrong).toEqual(unknown)
    await anonymous.dispose()
  })

  test('serves downloads as inert attachments whatever their name and content', async ({ request, quickId }) => {
    const { csrf, workspaceId } = await caller(request)
    const files = `/api/v1/workspaces/${workspaceId}/sites/${quickId}/files`
    const names = ['e2e-quote"semi;colon.txt', 'e2e-<img src=x onerror=alert(1)>.html', 'e2e-ñandú-日本語.txt']
    const remove = (name: string) => request.delete(files, { params: { path: `/upload/${name}` }, headers: { 'X-CSRF-Token': csrf } })

    for (const name of names) {
      const path = `/upload/${name}`
      const upload = await request.put(`${files}/content`, {
        params: { path, mode: 'overwrite' },
        data: Buffer.from('<script>alert(1)</script>'),
        headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/octet-stream' },
      })
      expect(upload.status(), name).toBe(201)
      const download = await request.get(`${files}/content`, { params: { path } })
      const headers = download.headers()
      expect(headers['content-disposition'], name).toMatch(/^attachment; filename/)
      expect(headers['content-disposition'], name).not.toMatch(/[\r\n]/)
      expect(headers['content-type'], name).toBe('application/octet-stream')
      expect(headers['x-content-type-options'], name).toBe('nosniff')
      expect(await download.text()).toBe('<script>alert(1)</script>')
      await remove(name)
    }
  })

  test('shows hostile names as text and runs nothing', async ({ shell, quickId, request }) => {
    const { page } = shell
    const { csrf, workspaceId } = await caller(request)
    const files = `/api/v1/workspaces/${workspaceId}/sites/${quickId}/files`
    const hostile = '<img src=x onerror="window.__xss=1">'
    const folder = `e2e-${Date.now()}-${hostile}`
    await request.post(`${files}/directories`, { data: { path: `/upload/${folder}` }, headers: { 'X-CSRF-Token': csrf } })

    await page.goto(`/sites/${quickId}?path=%2Fupload`)
    const row = page.getByRole('row').filter({ hasText: folder })
    await expect(row).toBeVisible()
    await expect(row.locator('img')).toHaveCount(0)
    await row.click()
    await expect(page.getByRole('complementary', { name: 'Details' })).toContainText(hostile)

    await page.getByLabel('Choose files to upload').setInputFiles({ name: `e2e-${Date.now()}-${hostile}.txt`, mimeType: 'text/plain', buffer: Buffer.from('x') })
    const dock = page.getByRole('region', { name: 'Transfers' })
    await dock.getByRole('tab', { name: /Finished/ }).click()
    await expect(dock.getByRole('listitem').filter({ hasText: hostile }).first()).toBeVisible()
    await expect(dock.locator('img')).toHaveCount(0)

    expect(await page.evaluate(() => (globalThis as { __xss?: number }).__xss)).toBeUndefined()

    await request.delete(files, { params: { path: `/upload/${folder}` }, headers: { 'X-CSRF-Token': csrf } })
    const uploaded = (await (await request.get(files, { params: { path: '/upload' } })).json()) as Array<{ name: string; path: string }>
    for (const entry of uploaded.filter((item) => item.name.startsWith('e2e-') && item.name.includes('onerror'))) {
      await request.delete(files, { params: { path: entry.path }, headers: { 'X-CSRF-Token': csrf } })
    }
  })
})
