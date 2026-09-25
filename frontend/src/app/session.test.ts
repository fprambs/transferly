import { QueryClient } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { createApi } from '@/lib/api'
import { HttpTransport } from '@/lib/transport'
import { adminSession, mockSession, problem } from '@/test/handlers'
import { server } from '@/test/msw'

import { sessionQuery, setupQuery } from './session'

/** Builds the pieces a query needs. */
function setup() {
  const transport = new HttpTransport()
  return { transport, api: createApi(transport), client: new QueryClient() }
}

/**
 * Covers {@link sessionQuery} and {@link setupQuery}.
 *
 * Scenarios:
 * - a live session is returned and its anti-CSRF token reaches the transport
 * - a 401 means nobody is signed in and forgets the token
 * - any other failure is thrown
 * - the setup status resolves to whether an administrator is still needed
 */
describe('session queries', () => {
  it('returns the session and hands the token to the transport', async () => {
    mockSession(adminSession)
    const { api, transport, client } = setup()
    let sent: string | null = null
    server.use(
      http.post('/api/v1/vault/lock', ({ request }) => {
        sent = request.headers.get('X-CSRF-Token')
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const session = await client.fetchQuery(sessionQuery(api, transport))
    await transport.request({ method: 'POST', path: '/api/v1/vault/lock' })

    expect(session).toEqual(adminSession)
    expect(sent).toBe('csrf-token-1')
  })

  it('returns null on 401 and forgets the token', async () => {
    mockSession(null)
    const { api, transport, client } = setup()
    transport.setCsrfToken('old')
    let sent: string | null = 'unset'
    server.use(
      http.post('/api/v1/vault/lock', ({ request }) => {
        sent = request.headers.get('X-CSRF-Token')
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const session = await client.fetchQuery(sessionQuery(api, transport))
    await transport.request({ method: 'POST', path: '/api/v1/vault/lock' })

    expect(session).toBeNull()
    expect(sent).toBeNull()
  })

  it('throws other failures', async () => {
    server.use(http.get('/api/v1/me', () => problem(500, 'internal')))
    const { api, transport, client } = setup()

    await expect(client.fetchQuery(sessionQuery(api, transport))).rejects.toMatchObject({ code: 'internal' })
  })

  it('resolves the setup status', async () => {
    server.use(http.get('/api/v1/setup/status', () => HttpResponse.json({ required: true })))
    const { api, client } = setup()

    await expect(client.fetchQuery(setupQuery(api))).resolves.toBe(true)
  })
})
