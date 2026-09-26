import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { server } from '@/test/msw'

import { HttpTransport } from '../transport'

import { createApi } from './index'

/** What the mock server saw of a call. */
interface Seen {
  /** HTTP method. */
  method: string
  /** Path of the URL. */
  path: string
  /** Query string with its question mark, or empty. */
  query: string
  /** Body as text. */
  body: string
  /** Anti-CSRF header, if any. */
  csrf: string | null
  /** Content type header, if any. */
  type: string | null
}

let seen: Seen[] = []

/** Answers every call with an empty JSON object and records what it received. */
function recordEverything(status = 200): void {
  server.use(
    http.all('*', async ({ request }) => {
      const url = new URL(request.url)
      seen.push({
        method: request.method,
        path: url.pathname,
        query: url.search,
        body: await request.text(),
        csrf: request.headers.get('X-CSRF-Token'),
        type: request.headers.get('Content-Type'),
      })
      return status === 204 ? new HttpResponse(null, { status }) : HttpResponse.json({})
    }),
  )
}

/** Builds a client whose transport already holds an anti-CSRF token. */
function client() {
  const transport = new HttpTransport()
  transport.setCsrfToken('csrf-1')
  return { api: createApi(transport), transport }
}

const ws = 'w1'
const site = 's1'
const files = `/api/v1/workspaces/${ws}/sites/${site}/files`

/**
 * Covers {@link createApi}: every endpoint sends the method, path, query and body the OpenAPI contract defines.
 *
 * Scenarios:
 * - each endpoint of authentication, the vault, workspaces, collections, sites, host keys, quick connections, files
 *   and transfers is called with the documented method and path
 * - bodies are JSON, query parameters are encoded and left-out ones are not sent
 * - state-changing calls carry the anti-CSRF token and reads do not
 * - uploads send the blob as it is with the resume options in the query, and the download URL is built without a request
 */
describe('createApi', () => {
  beforeEach(() => {
    seen = []
  })

  const cases: ReadonlyArray<{
    name: string
    call: (api: ReturnType<typeof client>['api']) => Promise<unknown>
    method: string
    path: string
    query?: string
    body?: unknown
  }> = [
    { name: 'auth.setupStatus', call: (a) => a.auth.setupStatus(), method: 'GET', path: '/api/v1/setup/status' },
    { name: 'auth.setup', call: (a) => a.auth.setup({ email: 'a@b.c', password: 'pw' }), method: 'POST', path: '/api/v1/setup', body: { email: 'a@b.c', password: 'pw' } },
    { name: 'auth.login', call: (a) => a.auth.login({ email: 'a@b.c', password: 'pw' }), method: 'POST', path: '/api/v1/auth/login', body: { email: 'a@b.c', password: 'pw' } },
    { name: 'auth.logout', call: (a) => a.auth.logout(), method: 'POST', path: '/api/v1/auth/logout' },
    { name: 'auth.me', call: (a) => a.auth.me(), method: 'GET', path: '/api/v1/me' },
    { name: 'auth.updateMe', call: (a) => a.auth.updateMe({ theme: 'nord' }), method: 'PATCH', path: '/api/v1/me', body: { theme: 'nord' } },
    { name: 'vault.status', call: (a) => a.vault.status(), method: 'GET', path: '/api/v1/vault' },
    { name: 'vault.setup', call: (a) => a.vault.setup('m'), method: 'POST', path: '/api/v1/vault/setup', body: { masterPassword: 'm' } },
    { name: 'vault.unlock', call: (a) => a.vault.unlock('m'), method: 'POST', path: '/api/v1/vault/unlock', body: { masterPassword: 'm' } },
    { name: 'vault.lock', call: (a) => a.vault.lock(), method: 'POST', path: '/api/v1/vault/lock' },
    { name: 'vault.changePassword', call: (a) => a.vault.changePassword('c', 'n'), method: 'POST', path: '/api/v1/vault/password', body: { currentPassword: 'c', newPassword: 'n' } },
    { name: 'vault.recover', call: (a) => a.vault.recover('k', 'n'), method: 'POST', path: '/api/v1/vault/recover', body: { recoveryKit: 'k', newPassword: 'n' } },
    { name: 'vault.regenerateRecoveryKit', call: (a) => a.vault.regenerateRecoveryKit('m'), method: 'POST', path: '/api/v1/vault/recovery-kit', body: { masterPassword: 'm' } },
    { name: 'workspaces.list', call: (a) => a.workspaces.list(), method: 'GET', path: '/api/v1/workspaces' },
    { name: 'workspaces.get', call: (a) => a.workspaces.get(ws), method: 'GET', path: `/api/v1/workspaces/${ws}` },
    { name: 'collections.list', call: (a) => a.collections.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/collections` },
    { name: 'collections.create', call: (a) => a.collections.create(ws, { name: 'Acme' }), method: 'POST', path: `/api/v1/workspaces/${ws}/collections`, body: { name: 'Acme' } },
    { name: 'collections.update', call: (a) => a.collections.update(ws, 'c1', { moveToTop: true }), method: 'PATCH', path: `/api/v1/workspaces/${ws}/collections/c1`, body: { moveToTop: true } },
    { name: 'collections.remove', call: (a) => a.collections.remove(ws, 'c1'), method: 'DELETE', path: `/api/v1/workspaces/${ws}/collections/c1` },
    { name: 'sites.list', call: (a) => a.sites.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/sites` },
    { name: 'sites.get', call: (a) => a.sites.get(ws, site), method: 'GET', path: `/api/v1/workspaces/${ws}/sites/${site}` },
    { name: 'sites.create', call: (a) => a.sites.create(ws, { name: 'S', protocol: 'sftp', host: 'h', username: 'u' }), method: 'POST', path: `/api/v1/workspaces/${ws}/sites`, body: { name: 'S', protocol: 'sftp', host: 'h', username: 'u' } },
    { name: 'sites.update', call: (a) => a.sites.update(ws, site, { name: 'S', protocol: 'sftp', host: 'h', username: 'u' }), method: 'PUT', path: `/api/v1/workspaces/${ws}/sites/${site}`, body: { name: 'S', protocol: 'sftp', host: 'h', username: 'u' } },
    { name: 'sites.remove', call: (a) => a.sites.remove(ws, site), method: 'DELETE', path: `/api/v1/workspaces/${ws}/sites/${site}` },
    { name: 'hostKeys.list', call: (a) => a.hostKeys.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/known-hosts` },
    { name: 'hostKeys.trust', call: (a) => a.hostKeys.trust(ws, { host: 'h', port: 22, keyType: 'ssh-ed25519', fingerprint: 'SHA256:x' }), method: 'POST', path: `/api/v1/workspaces/${ws}/known-hosts`, body: { host: 'h', port: 22, keyType: 'ssh-ed25519', fingerprint: 'SHA256:x' } },
    { name: 'hostKeys.forget', call: (a) => a.hostKeys.forget(ws, { host: 'h', port: 22, keyType: 'ssh-ed25519' }), method: 'DELETE', path: `/api/v1/workspaces/${ws}/known-hosts`, query: '?host=h&port=22&keyType=ssh-ed25519' },
    { name: 'quick.list', call: (a) => a.quick.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/quick-connections` },
    { name: 'quick.create', call: (a) => a.quick.create(ws, { protocol: 'sftp', host: 'h', username: 'u' }), method: 'POST', path: `/api/v1/workspaces/${ws}/quick-connections`, body: { protocol: 'sftp', host: 'h', username: 'u' } },
    { name: 'quick.close', call: (a) => a.quick.close(ws, 'q1'), method: 'DELETE', path: `/api/v1/workspaces/${ws}/quick-connections/q1` },
    { name: 'quick.save', call: (a) => a.quick.save(ws, 'q1', { name: 'N', collectionId: 'c1' }), method: 'POST', path: `/api/v1/workspaces/${ws}/quick-connections/q1/save`, body: { name: 'N', collectionId: 'c1' } },
    { name: 'files.list', call: (a) => a.files.list(ws, site, '/a b'), method: 'GET', path: files, query: '?path=%2Fa+b' },
    { name: 'files.stat', call: (a) => a.files.stat(ws, site, '/f'), method: 'GET', path: `${files}/entry`, query: '?path=%2Ff' },
    { name: 'files.mkdir', call: (a) => a.files.mkdir(ws, site, '/d'), method: 'POST', path: `${files}/directories`, body: { path: '/d' } },
    { name: 'files.rename', call: (a) => a.files.rename(ws, site, '/a', '/b'), method: 'POST', path: `${files}/rename`, body: { from: '/a', to: '/b' } },
    { name: 'files.remove', call: (a) => a.files.remove(ws, site, '/d'), method: 'DELETE', path: files, query: '?path=%2Fd' },
    { name: 'files.remove recursive', call: (a) => a.files.remove(ws, site, '/d', true), method: 'DELETE', path: files, query: '?path=%2Fd&recursive=true' },
    { name: 'copies.create', call: (a) => a.copies.create(ws, { sourceSiteId: 's1', sourcePath: '/a', targetSiteId: 's2', targetPath: '/b', overwrite: true }), method: 'POST', path: `/api/v1/workspaces/${ws}/copies`, body: { sourceSiteId: 's1', sourcePath: '/a', targetSiteId: 's2', targetPath: '/b', overwrite: true } },
    { name: 'transfers.list', call: (a) => a.transfers.list(), method: 'GET', path: '/api/v1/transfers' },
    { name: 'transfers.list filtered', call: (a) => a.transfers.list({ workspaceId: ws, limit: 5 }), method: 'GET', path: '/api/v1/transfers', query: '?workspaceId=w1&limit=5' },
    { name: 'transfers.get', call: (a) => a.transfers.get('t1'), method: 'GET', path: '/api/v1/transfers/t1' },
    { name: 'transfers.remove', call: (a) => a.transfers.remove('t1'), method: 'DELETE', path: '/api/v1/transfers/t1' },
    { name: 'transfers.cancel', call: (a) => a.transfers.cancel('t1'), method: 'POST', path: '/api/v1/transfers/t1/cancel' },
    { name: 'transfers.pause', call: (a) => a.transfers.pause('t1'), method: 'POST', path: '/api/v1/transfers/t1/pause' },
    { name: 'transfers.resume', call: (a) => a.transfers.resume('t1'), method: 'POST', path: '/api/v1/transfers/t1/resume' },
    { name: 'transfers.setPriority', call: (a) => a.transfers.setPriority('t1', 1), method: 'PATCH', path: '/api/v1/transfers/t1', body: { priority: 1 } },
  ]

  it.each(cases)('calls $name', async ({ call, method, path, query, body }) => {
    recordEverything()

    await call(client().api)

    expect(seen).toHaveLength(1)
    expect(seen[0]).toMatchObject({ method, path, query: query ?? '' })
    if (body === undefined) {
      expect(seen[0]?.body).toBe('')
    } else {
      expect(JSON.parse(seen[0]?.body ?? '')).toEqual(body)
      expect(seen[0]?.type).toBe('application/json')
    }
    expect(seen[0]?.csrf).toBe(method === 'GET' ? null : 'csrf-1')
  })

  it('uploads a blob as it is with the resume options in the query', async () => {
    recordEverything()
    const { api } = client()

    await api.files.upload(ws, site, '/big.bin', new TextEncoder().encode('0123456789'), { mode: 'resume', offset: 4, transferId: 't9', size: 10 })

    expect(seen[0]).toMatchObject({ method: 'PUT', path: `${files}/content`, body: '0123456789', type: 'application/octet-stream', csrf: 'csrf-1' })
    expect(new URLSearchParams(seen[0]?.query)).toEqual(new URLSearchParams('path=%2Fbig.bin&mode=resume&offset=4&transferId=t9&size=10'))
  })

  it('uploads with only the path when no options are given', async () => {
    recordEverything()

    await client().api.files.upload(ws, site, '/f', new TextEncoder().encode('x'))

    expect(seen[0]?.query).toBe('?path=%2Ff')
  })

  it('streams a download with the transfer identifier in the query', async () => {
    recordEverything()

    await client().api.files.download(ws, site, '/a b', 't1')

    expect(seen[0]).toMatchObject({ method: 'GET', path: `${files}/content`, query: '?path=%2Fa+b&transferId=t1' })
  })

  it('builds download URLs without calling the server', () => {
    const { api } = client()

    expect(api.files.downloadUrl(ws, site, '/a/b.txt')).toBe(`${files}/content?path=%2Fa%2Fb.txt`)
    expect(api.files.downloadUrl(ws, site, '/f', 't1')).toBe(`${files}/content?path=%2Ff&transferId=t1`)
    expect(api.files.archiveUrl(ws, site, '/d e')).toBe(`${files}/archive?path=%2Fd+e`)
    expect(api.files.archiveUrl(ws, site, '/d', 't1')).toBe(`${files}/archive?path=%2Fd&transferId=t1`)
    expect(seen).toHaveLength(0)
  })

  it('sends no token before one is set, or after it was forgotten', async () => {
    recordEverything(204)
    const transport = new HttpTransport()
    const api = createApi(transport)

    await api.auth.logout()
    transport.setCsrfToken('later')
    await api.auth.logout()
    transport.setCsrfToken(undefined)
    await api.auth.logout()

    expect(seen.map((call) => call.csrf)).toEqual([null, 'later', null])
  })

  it('returns nothing for answers without a body and the document for the rest', async () => {
    recordEverything(204)
    await expect(client().api.auth.logout()).resolves.toBeUndefined()

    server.resetHandlers()
    server.use(http.get('*/api/v1/me', () => HttpResponse.json({ csrfToken: 'x' })))
    await expect(client().api.auth.me()).resolves.toEqual({ csrfToken: 'x' })
  })
})
