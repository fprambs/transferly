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
  /** FileZilla master password header, if any. */
  master: string | null
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
        master: request.headers.get('X-FileZilla-Master-Password'),
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
 * - each endpoint of authentication, the vault, workspaces, members, invitations, collections, sites, host keys, quick connections, files
 *   and transfers is called with the documented method and path
 * - bodies are JSON, query parameters are encoded and left-out ones are not sent
 * - state-changing calls carry the anti-CSRF token and reads do not
 * - uploads send the blob as it is with the resume options in the query, and the download URLs are built without a request
 * - a site manager goes as XML, with its FileZilla master password in a header only when one is given
 * - ranges of files are read as bytes and entries of archives streamed, with the chain of nested archives as repeated parameters
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
    { name: 'auth.verifyMfa', call: (a) => a.auth.verifyMfa('c', '123456'), method: 'POST', path: '/api/v1/auth/mfa', body: { challenge: 'c', code: '123456' } },
    { name: 'auth.beginPasskeyLogin', call: (a) => a.auth.beginPasskeyLogin('c'), method: 'POST', path: '/api/v1/auth/passkey/begin', body: { challenge: 'c' } },
    { name: 'auth.finishPasskeyLogin', call: (a) => a.auth.finishPasskeyLogin('c', 'z', { id: 'k' }), method: 'POST', path: '/api/v1/auth/passkey/finish', body: { challenge: 'c', ceremony: 'z', credential: { id: 'k' } } },
    { name: 'auth.beginPasskey', call: (a) => a.auth.beginPasskey(), method: 'POST', path: '/api/v1/me/passkeys/begin' },
    { name: 'auth.finishPasskey', call: (a) => a.auth.finishPasskey('z', 'Laptop', { id: 'k' }), method: 'POST', path: '/api/v1/me/passkeys/finish', body: { ceremony: 'z', name: 'Laptop', credential: { id: 'k' } } },
    { name: 'auth.removePasskey', call: (a) => a.auth.removePasskey('p1', 'pw'), method: 'POST', path: '/api/v1/me/passkeys/p1/remove', body: { password: 'pw' } },
    { name: 'auth.security', call: (a) => a.auth.security(), method: 'GET', path: '/api/v1/me/security' },
    { name: 'auth.beginTotp', call: (a) => a.auth.beginTotp(), method: 'POST', path: '/api/v1/me/totp' },
    { name: 'auth.confirmTotp', call: (a) => a.auth.confirmTotp('123456'), method: 'POST', path: '/api/v1/me/totp/confirm', body: { code: '123456' } },
    { name: 'auth.disableTotp', call: (a) => a.auth.disableTotp('pw'), method: 'POST', path: '/api/v1/me/totp/disable', body: { password: 'pw' } },
    { name: 'auth.regenerateRecoveryCodes', call: (a) => a.auth.regenerateRecoveryCodes('pw'), method: 'POST', path: '/api/v1/me/recovery-codes', body: { password: 'pw' } },
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
    { name: 'workspaces.create', call: (a) => a.workspaces.create('Acme'), method: 'POST', path: '/api/v1/workspaces', body: { name: 'Acme' } },
    { name: 'workspaces.members', call: (a) => a.workspaces.members(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/members` },
    { name: 'workspaces.setRole', call: (a) => a.workspaces.setRole(ws, 'u1', 'editor'), method: 'PATCH', path: `/api/v1/workspaces/${ws}/members/u1`, body: { role: 'editor' } },
    { name: 'workspaces.removeMember', call: (a) => a.workspaces.removeMember(ws, 'u1'), method: 'DELETE', path: `/api/v1/workspaces/${ws}/members/u1` },
    { name: 'workspaces.leave', call: (a) => a.workspaces.leave(ws), method: 'POST', path: `/api/v1/workspaces/${ws}/leave` },
    { name: 'workspaces.transfer', call: (a) => a.workspaces.transfer(ws, 'u1'), method: 'POST', path: `/api/v1/workspaces/${ws}/transfer`, body: { userId: 'u1' } },
    { name: 'audit.list', call: (a) => a.audit.list(ws, { action: 'member.', before: 9, limit: 20 }), method: 'GET', path: `/api/v1/workspaces/${ws}/audit`, query: '?action=member.&before=9&limit=20' },
    { name: 'invitations.list', call: (a) => a.invitations.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/invitations` },
    { name: 'invitations.create', call: (a) => a.invitations.create(ws, { email: 'a@b.co', role: 'viewer' }), method: 'POST', path: `/api/v1/workspaces/${ws}/invitations`, body: { email: 'a@b.co', role: 'viewer' } },
    { name: 'invitations.revoke', call: (a) => a.invitations.revoke(ws, 'i1'), method: 'DELETE', path: `/api/v1/workspaces/${ws}/invitations/i1` },
    { name: 'invitations.preview', call: (a) => a.invitations.preview('tok'), method: 'POST', path: '/api/v1/invitations/preview', body: { token: 'tok' } },
    { name: 'invitations.accept', call: (a) => a.invitations.accept('tok'), method: 'POST', path: '/api/v1/invitations/accept', body: { token: 'tok' } },
    { name: 'invitations.join', call: (a) => a.invitations.join('tok', 'pw'), method: 'POST', path: '/api/v1/invitations/join', body: { token: 'tok', password: 'pw' } },
    { name: 'collections.list', call: (a) => a.collections.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/collections` },
    { name: 'collections.create', call: (a) => a.collections.create(ws, { name: 'Acme' }), method: 'POST', path: `/api/v1/workspaces/${ws}/collections`, body: { name: 'Acme' } },
    { name: 'collections.update', call: (a) => a.collections.update(ws, 'c1', { moveToTop: true }), method: 'PATCH', path: `/api/v1/workspaces/${ws}/collections/c1`, body: { moveToTop: true } },
    { name: 'collections.remove', call: (a) => a.collections.remove(ws, 'c1'), method: 'DELETE', path: `/api/v1/workspaces/${ws}/collections/c1` },
    { name: 'sites.list', call: (a) => a.sites.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/sites` },
    { name: 'sites.get', call: (a) => a.sites.get(ws, site), method: 'GET', path: `/api/v1/workspaces/${ws}/sites/${site}` },
    { name: 'sshKeys.inspect', call: (a) => a.sshKeys.inspect('KEY', 'phrase'), method: 'POST', path: '/api/v1/ssh-keys/inspect', body: { key: 'KEY', passphrase: 'phrase' } },
    { name: 'sites.disconnect', call: (a) => a.sites.disconnect(ws, 's1'), method: 'DELETE', path: `/api/v1/workspaces/${ws}/sites/s1/connection` },
    { name: 'connections.list', call: (a) => a.connections.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/connections` },
    { name: 'sites.create', call: (a) => a.sites.create(ws, { connector: 'sftp', config: { host: 'h', username: 'u' }, name: 'S' }), method: 'POST', path: `/api/v1/workspaces/${ws}/sites`, body: { connector: 'sftp', config: { host: 'h', username: 'u' }, name: 'S' } },
    { name: 'sites.update', call: (a) => a.sites.update(ws, site, { connector: 'sftp', config: { host: 'h', username: 'u' }, name: 'S' }), method: 'PUT', path: `/api/v1/workspaces/${ws}/sites/${site}`, body: { connector: 'sftp', config: { host: 'h', username: 'u' }, name: 'S' } },
    { name: 'sites.remove', call: (a) => a.sites.remove(ws, site), method: 'DELETE', path: `/api/v1/workspaces/${ws}/sites/${site}` },
    { name: 'prompts.answer', call: (a) => a.prompts.answer('p1', ['secret', '123456']), method: 'POST', path: '/api/v1/prompts/p1', body: { answers: ['secret', '123456'] } },
    { name: 'prompts.cancel', call: (a) => a.prompts.cancel('p1'), method: 'DELETE', path: '/api/v1/prompts/p1' },
    { name: 'hostKeys.list', call: (a) => a.hostKeys.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/known-hosts` },
    { name: 'hostKeys.trust', call: (a) => a.hostKeys.trust(ws, { host: 'h', port: 22, keyType: 'ssh-ed25519', fingerprint: 'SHA256:x' }), method: 'POST', path: `/api/v1/workspaces/${ws}/known-hosts`, body: { host: 'h', port: 22, keyType: 'ssh-ed25519', fingerprint: 'SHA256:x' } },
    { name: 'hostKeys.forget', call: (a) => a.hostKeys.forget(ws, { host: 'h', port: 22, keyType: 'ssh-ed25519' }), method: 'DELETE', path: `/api/v1/workspaces/${ws}/known-hosts`, query: '?host=h&port=22&keyType=ssh-ed25519' },
    { name: 'quick.list', call: (a) => a.quick.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/quick-connections` },
    { name: 'quick.create', call: (a) => a.quick.create(ws, { connector: 'sftp', config: { host: 'h', username: 'u' } }), method: 'POST', path: `/api/v1/workspaces/${ws}/quick-connections`, body: { connector: 'sftp', config: { host: 'h', username: 'u' } } },
    { name: 'quick.close', call: (a) => a.quick.close(ws, 'q1'), method: 'DELETE', path: `/api/v1/workspaces/${ws}/quick-connections/q1` },
    { name: 'quick.save', call: (a) => a.quick.save(ws, 'q1', { name: 'N', collectionId: 'c1' }), method: 'POST', path: `/api/v1/workspaces/${ws}/quick-connections/q1/save`, body: { name: 'N', collectionId: 'c1' } },
    { name: 'files.list', call: (a) => a.files.list(ws, site, '/a b'), method: 'GET', path: files, query: '?path=%2Fa+b' },
    { name: 'files.probeTimeZone', call: (a) => a.files.probeTimeZone(ws, site, '/dir'), method: 'POST', path: `${files}/timezone-probe`, query: '?path=%2Fdir' },
    { name: 'connectors.list', call: (a) => a.connectors.list(), method: 'GET', path: '/api/v1/connectors', query: '' },
    {
      name: 'connectors.setEnabled',
      call: (a) => a.connectors.setEnabled('s3/x', false),
      method: 'PUT',
      path: '/api/v1/admin/connectors/s3%2Fx',
      query: '',
      body: { enabled: false },
    },
    { name: 'files.archiveEntries', call: (a) => a.files.archiveEntries(ws, site, '/a.zip', ['in.zip']), method: 'GET', path: `${files}/archive-entries`, query: '?path=%2Fa.zip&inner=in.zip' },
    { name: 'files.checksum', call: (a) => a.files.checksum(ws, site, '/f', 'md5'), method: 'GET', path: `${files}/checksum`, query: '?path=%2Ff&algorithm=md5' },
    { name: 'files.stat', call: (a) => a.files.stat(ws, site, '/f'), method: 'GET', path: `${files}/entry`, query: '?path=%2Ff' },
    { name: 'files.mkdir', call: (a) => a.files.mkdir(ws, site, '/d'), method: 'POST', path: `${files}/directories`, body: { path: '/d' } },
    { name: 'files.tools', call: (a) => a.files.tools(ws, site), method: 'GET', path: `/api/v1/workspaces/${ws}/sites/${site}/tools` },
    { name: 'files.usage', call: (a) => a.files.usage(ws, site, '/d'), method: 'GET', path: `${files}/usage`, query: '?path=%2Fd' },
    { name: 'files.compress', call: (a) => a.files.compress(ws, site, '/', ['a'], 'a.zip', 'zip'), method: 'POST', path: `${files}/compress`, body: { dir: '/', names: ['a'], name: 'a.zip', format: 'zip' } },
    { name: 'files.extract', call: (a) => a.files.extract(ws, site, '/a.zip'), method: 'POST', path: `${files}/extract`, body: { path: '/a.zip' } },
    { name: 'files.rename', call: (a) => a.files.rename(ws, site, '/a', '/b'), method: 'POST', path: `${files}/rename`, body: { from: '/a', to: '/b' } },
    { name: 'files.remove', call: (a) => a.files.remove(ws, site, '/d'), method: 'DELETE', path: files, query: '?path=%2Fd' },
    { name: 'files.remove recursive', call: (a) => a.files.remove(ws, site, '/d', true), method: 'DELETE', path: files, query: '?path=%2Fd&recursive=true' },
    { name: 'activity.list', call: (a) => a.activity.list(ws), method: 'GET', path: `/api/v1/workspaces/${ws}/activity` },
    { name: 'activity.list filtered', call: (a) => a.activity.list(ws, { siteId: 's1', limit: 20 }), method: 'GET', path: `/api/v1/workspaces/${ws}/activity`, query: '?siteId=s1&limit=20' },
    { name: 'copies.create', call: (a) => a.copies.create(ws, { sourceSiteId: 's1', sourcePath: '/a', targetSiteId: 's2', targetPath: '/b', policy: 'overwrite', verify: true }), method: 'POST', path: `/api/v1/workspaces/${ws}/copies`, body: { sourceSiteId: 's1', sourcePath: '/a', targetSiteId: 's2', targetPath: '/b', policy: 'overwrite', verify: true } },
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

  it('sends a site manager file as XML with the import options, and its master password only when given', async () => {
    recordEverything()

    await client().api.imports.filezilla(ws, new TextEncoder().encode('<FileZilla3/>'), { dryRun: true, passwords: false, onConflict: 'rename' })

    expect(seen[0]).toMatchObject({ method: 'POST', path: `/api/v1/workspaces/${ws}/imports/filezilla`, query: '?dryRun=true&passwords=false&onConflict=rename', type: 'application/xml' })
    expect(seen[0]?.body).toBe('<FileZilla3/>')
    expect(seen[0]?.master).toBeNull()

    await client().api.imports.filezilla(ws, new TextEncoder().encode('<FileZilla3/>'), { filezillaMasterPassword: '' })
    expect(seen[1]?.master).toBeNull()
    await client().api.imports.filezilla(ws, new TextEncoder().encode('<FileZilla3/>'), { filezillaMasterPassword: 'master' })
    expect(seen[2]?.master).toBe('master')
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

  it('reads a range of a file as bytes', async () => {
    let range: string | null = null
    server.use(
      http.get(`${files}/content`, ({ request }) => {
        range = request.headers.get('Range')
        return new HttpResponse(new Uint8Array([1, 2, 3]), { status: 206 })
      }),
    )

    const bytes = await client().api.files.read(ws, site, '/a.bin', 10, 13)

    expect([...bytes]).toEqual([1, 2, 3])
    expect(range).toBe('bytes=10-12')
  })

  it('streams an entry of an archive', async () => {
    let query = ''
    server.use(
      http.get(`${files}/archive-entry`, ({ request }) => {
        query = new URL(request.url).search
        return new HttpResponse(new Uint8Array([4, 5]))
      }),
    )

    const bytes = new Uint8Array(await new Response(await client().api.files.archiveEntry(ws, site, '/a.zip', ['b.zip'], 'c.txt')).arrayBuffer())

    expect([...bytes]).toEqual([4, 5])
    expect(query).toBe('?path=%2Fa.zip&inner=b.zip&entry=c.txt')
  })

  it('builds download URLs without calling the server', () => {
    const { api } = client()

    expect(api.files.downloadUrl(ws, site, '/a/b.txt')).toBe(`${files}/content?path=%2Fa%2Fb.txt`)
    expect(api.files.downloadUrl(ws, site, '/f', 't1')).toBe(`${files}/content?path=%2Ff&transferId=t1`)
    expect(api.files.archiveUrl(ws, site, '/d e')).toBe(`${files}/archive?path=%2Fd+e`)
    expect(api.files.archiveUrl(ws, site, '/d', 't1')).toBe(`${files}/archive?path=%2Fd&transferId=t1`)
    expect(api.files.archiveEntryUrl(ws, site, '/a.zip', [], 'x/y.txt')).toBe(`${files}/archive-entry?path=%2Fa.zip&entry=x%2Fy.txt`)
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
