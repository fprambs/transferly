import { http, HttpResponse } from 'msw'

import type { AdminUser, AuditEvent, Networks } from '@/lib/api'

import { adminSession, mockSession, mockWorkspaceData, problem } from './handlers'
import { server } from './msw'
import type { SeenCall } from './teams'

/** Builds an account of the instance. */
export function adminUser(id: string, email: string, extra: Partial<AdminUser> = {}): AdminUser {
  return { id, email, role: 'user', disabled: false, secondFactor: false, createdAt: '2026-03-10T12:00:00Z', ...extra }
}

/** The accounts of the instance unless a test says otherwise: the signed-in administrator and three more. */
export const defaultUsers: AdminUser[] = [
  adminUser(adminSession.user.id, adminSession.user.email, { role: 'admin' }),
  adminUser('u-maria', 'maria@acme.dev', { secondFactor: true }),
  adminUser('u-jorge', 'jorge@acme.dev', { disabled: true }),
  adminUser('u-lucia', 'lucia@agency.io', { role: 'admin' }),
]

/** What a test can ask for when it mocks the administration API. */
export interface AdminOptions {
  /** The accounts the server holds. */
  users?: AdminUser[]
  /** The networks the server holds. */
  networks?: Networks
  /** Answers the audit trail of the instance for a query. */
  audit?: (query: URLSearchParams) => AuditEvent[]
}

/** State of the mocked administration API: what it holds now and what was asked of it. */
export interface AdminMock {
  /** The accounts the server holds. */
  users: AdminUser[]
  /** The networks the server holds. */
  networks: Networks
  /** Every change asked for. */
  calls: SeenCall[]
  /** The queries the audit trail was read with. */
  auditQueries: URLSearchParams[]
}

/**
 * Signs in as an administrator and mocks the administration API. The lists change with the calls, so what the screen reads after a
 * change is what the server would answer.
 */
export function mockAdmin({ users = defaultUsers, networks = { fixed: ['172.16.0.0/12'], custom: ['192.168.1.0/24'] }, audit = () => [] }: AdminOptions = {}): AdminMock {
  mockSession(adminSession)
  mockWorkspaceData()
  const state: AdminMock = { users: [...users], networks, calls: [], auditQueries: [] }
  const record = async (request: Request): Promise<unknown> => {
    const text = await request.text()
    const body = text === '' ? undefined : (JSON.parse(text) as unknown)
    state.calls.push({ method: request.method, path: new URL(request.url).pathname, body })
    return body
  }
  server.use(
    http.get('/api/v1/admin/users', () => HttpResponse.json(state.users)),
    http.patch('/api/v1/admin/users/:id', async ({ request, params }) => {
      const body = (await record(request)) as Partial<AdminUser>
      state.users = state.users.map((u) => (u.id === params.id ? { ...u, ...body } : u))
      return new HttpResponse(null, { status: 204 })
    }),
    http.post('/api/v1/admin/users/:id/second-factor/reset', async ({ request, params }) => {
      await record(request)
      state.users = state.users.map((u) => (u.id === params.id ? { ...u, secondFactor: false } : u))
      return new HttpResponse(null, { status: 204 })
    }),
    http.get('/api/v1/admin/networks', () => HttpResponse.json(state.networks)),
    http.put('/api/v1/admin/networks', async ({ request }) => {
      const body = (await record(request)) as { networks: string[] }
      state.networks = { ...state.networks, custom: body.networks.map((n) => (n.includes('/') ? n : `${n}/32`)) }
      return HttpResponse.json(state.networks)
    }),
    http.get('/api/v1/admin/audit', ({ request }) => {
      const query = new URL(request.url).searchParams
      state.auditQueries.push(query)
      return HttpResponse.json(audit(query))
    }),
  )
  return state
}

/** Makes a route of the administration API answer with a problem instead. */
export function failAdmin(method: 'patch' | 'post' | 'put', path: string, status: number, code: string): void {
  server.use(http[method](`/api/v1/admin${path}`, () => problem(status, code)))
}
