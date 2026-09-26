import { http, HttpResponse } from 'msw'

import type { Invitation, Member, Workspace } from '@/lib/api'

import { adminSession, mockSession, mockWorkspaceData, personalWorkspace, problem } from './handlers'
import { server } from './msw'

/** Identifier of the team workspace of the mock server. */
export const teamId = '7d2f3c1e-9a44-4b7a-8c0e-2f5d1b6a9e01'

/** Builds the team workspace as a member with a role sees it. */
export function teamWorkspace(role: Workspace['role']): Workspace {
  return { id: teamId, name: 'Acme Web', kind: 'team', role, createdAt: '2026-02-01T00:00:00Z' }
}

/** Builds a member. */
export function member(userId: string, email: string, role: Member['role'], keyState: Member['keyState'] = 'sealed'): Member {
  return { userId, email, role, keyState, joinedAt: '2026-03-10T12:00:00Z' }
}

/** Identifier of the signed-in administrator, who is a member of the team under the role a test chooses. */
export const meId = adminSession.user.id

/** What a test can ask for when it mocks the team. */
export interface TeamOptions {
  /** Role of the signed-in user in the team. */
  role?: Workspace['role']
  /** The other members of the team. */
  others?: Member[]
  /** Invitations waiting in the team. */
  invitations?: Invitation[]
}

/** The people who belong to the team unless a test says otherwise. */
export const defaultOthers: Member[] = [
  member('u-maria', 'maria@acme.dev', 'admin'),
  member('u-jorge', 'jorge@acme.dev', 'editor'),
  member('u-lucia', 'lucia@agency.io', 'editor', 'waiting'),
  member('u-pablo', 'pablo@acme.dev', 'viewer'),
]

/** A call the mock server saw. */
export interface SeenCall {
  /** HTTP method. */
  method: string
  /** Path of the URL. */
  path: string
  /** Body, parsed, or `undefined` when there was none. */
  body: unknown
}

/** State of the mocked team: what it holds now and what was asked of it. */
export interface TeamMock {
  /** The members the server holds. */
  members: Member[]
  /** The invitations the server holds. */
  invitations: Invitation[]
  /** Every change asked for. */
  calls: SeenCall[]
}

/**
 * Signs in and mocks a team workspace of the signed-in user: the workspaces, its members and invitations, and every change to them.
 * The lists change with the calls, so what the screen reads after a change is what the server would answer.
 */
export function mockTeam({ role = 'owner', others = defaultOthers, invitations = [] }: TeamOptions = {}): TeamMock {
  mockSession(adminSession)
  mockWorkspaceData({ workspaces: [personalWorkspace, teamWorkspace(role)] })
  const state: TeamMock = { members: [member(meId, adminSession.user.email, role), ...others], invitations: [...invitations], calls: [] }
  const base = `/api/v1/workspaces/${teamId}`
  const record = async (request: Request): Promise<unknown> => {
    const text = await request.text()
    const body = text === '' ? undefined : (JSON.parse(text) as unknown)
    state.calls.push({ method: request.method, path: new URL(request.url).pathname, body })
    return body
  }
  server.use(
    http.get(`${base}/members`, () => HttpResponse.json(state.members)),
    http.get(`${base}/invitations`, () => HttpResponse.json(state.invitations)),
    http.patch(`${base}/members/:userId`, async ({ request, params }) => {
      const body = (await record(request)) as { role: Member['role'] }
      state.members = state.members.map((m) => (m.userId === params.userId ? { ...m, role: body.role } : m))
      return new HttpResponse(null, { status: 204 })
    }),
    http.delete(`${base}/members/:userId`, async ({ request, params }) => {
      await record(request)
      state.members = state.members.filter((m) => m.userId !== params.userId)
      return new HttpResponse(null, { status: 204 })
    }),
    http.post(`${base}/leave`, async ({ request }) => {
      await record(request)
      return new HttpResponse(null, { status: 204 })
    }),
    http.post(`${base}/transfer`, async ({ request }) => {
      await record(request)
      return new HttpResponse(null, { status: 204 })
    }),
    http.post(`${base}/invitations`, async ({ request }) => {
      const body = (await record(request)) as { email: string; role: Invitation['role'] }
      const invitation: Invitation = { id: 'i-new', email: body.email, role: body.role, invitedBy: adminSession.user.email, createdAt: '2026-09-26T12:00:00Z', expiresAt: '2026-10-03T12:00:00Z' }
      state.invitations = [invitation, ...state.invitations]
      return HttpResponse.json({ invitation, token: 'tok-new-0123456789' }, { status: 201 })
    }),
    http.delete(`${base}/invitations/:id`, async ({ request, params }) => {
      await record(request)
      state.invitations = state.invitations.filter((i) => i.id !== params.id)
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return state
}

/** Makes a route of the team answer with a problem instead. */
export function failTeam(method: 'patch' | 'delete' | 'post', path: string, status: number, code: string): void {
  server.use(http[method](`/api/v1/workspaces/${teamId}${path}`, () => problem(status, code)))
}
