import { http, HttpResponse } from 'msw'

import type { VaultStatus, Collection, QuickConnection, SessionInfo, Site, Workspace } from '@/lib/api'
import { server } from './msw'

/** A signed-in administrator with an anti-CSRF token. */
export const adminSession: SessionInfo = {
  user: {
    id: '5b1d0c6e-7a43-4f7e-9a51-0c3d2a6b8e11',
    email: 'admin@example.com',
    role: 'admin',
    locale: 'en',
    timezone: 'UTC',
    theme: 'sentinel-dark',
  },
  csrfToken: 'csrf-token-1',
}

/** Builds an RFC 9457 problem answer. */
export function problem(status: number, code: string) {
  return HttpResponse.json(
    { type: 'about:blank', title: code, status, code },
    { status, headers: { 'Content-Type': 'application/problem+json' } },
  )
}

/** Answers `GET /me` with a session, or with 401 when there is none. */
export function mockSession(session: SessionInfo | null): void {
  server.use(http.get('/api/v1/me', () => (session === null ? problem(401, 'unauthenticated') : HttpResponse.json(session))))
}

/** Answers the setup status endpoint. */
export function mockSetupRequired(required: boolean): void {
  server.use(http.get('/api/v1/setup/status', () => HttpResponse.json({ required })))
}

/** Personal workspace of the signed-in administrator. */
export const personalWorkspace: Workspace = {
  id: '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10',
  name: 'Personal',
  kind: 'personal',
  role: 'owner',
  createdAt: '2026-01-01T00:00:00Z',
}

/** Contents a workspace can be mocked with. */
export interface WorkspaceData {
  /** Workspaces the user belongs to. */
  workspaces?: Workspace[]
  /** Collections of the workspace. */
  collections?: Collection[]
  /** Saved sites. */
  sites?: Site[]
  /** Open quick connections. */
  quick?: QuickConnection[]
}

/** Answers the workspace, collection, site and quick connection lists; whatever is not given is empty. */
export function mockWorkspaceData({ workspaces = [personalWorkspace], collections = [], sites = [], quick = [] }: WorkspaceData = {}): void {
  mockVault()
  server.use(
    http.get('/api/v1/workspaces', () => HttpResponse.json(workspaces)),
    http.get('/api/v1/workspaces/:id/collections', () => HttpResponse.json(collections)),
    http.get('/api/v1/workspaces/:id/sites', () => HttpResponse.json(sites)),
    http.get('/api/v1/workspaces/:id/quick-connections', () => HttpResponse.json(quick)),
  )
}

/** Answers the vault state endpoint. */
export function mockVault(status: VaultStatus = { initialized: true, unlocked: true }): void {
  server.use(http.get('/api/v1/vault', () => HttpResponse.json(status)))
}

/** Answers `PATCH /me`, echoing the account with the preferences it received; the bodies are recorded in the returned list. */
export function mockUpdateMe(): unknown[] {
  const received: unknown[] = []
  server.use(
    http.patch('/api/v1/me', async ({ request }) => {
      const body = (await request.json()) as Record<string, string>
      received.push(body)
      return HttpResponse.json({ ...adminSession.user, ...body })
    }),
  )
  return received
}

/**
 * Answers the vault state as locked, or as missing when `initialized` is false, and switches it to unlocked once the master password
 * is sent. The returned function tells whether that happened.
 */
export function mockUnlocking(initialized = true): { unlocked: () => boolean } {
  let unlocked = false
  server.use(
    http.get('/api/v1/vault', () => HttpResponse.json({ initialized, unlocked })),
    http.post('/api/v1/vault/unlock', () => {
      unlocked = true
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return { unlocked: () => unlocked }
}

/** Answers the second factor state of the account as off. */
export function mockSecurity(): void {
  server.use(http.get('/api/v1/me/security', () => HttpResponse.json({ totp: false, recoveryCodesLeft: 0 })))
}
