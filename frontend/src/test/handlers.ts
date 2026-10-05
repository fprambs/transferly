import { http, HttpResponse } from 'msw'

import type { Bookmark, Connector, VaultStatus, Collection, QuickConnection, SessionInfo, Site, Workspace, ServerTools } from '@/lib/api'
import catalog from './connectors.json'
import { server } from './msw'

/** A signed-in administrator with an anti-CSRF token. */
export const adminSession: SessionInfo = {
  user: {
    id: '5b1d0c6e-7a43-4f7e-9a51-0c3d2a6b8e11',
    email: 'admin@example.com',
    role: 'admin',
    locale: 'en',
    timezone: 'UTC',
    theme: 'transferly-dark',
    fileDoubleClick: 'download',
  },
  csrfToken: 'csrf-token-1',
}

/** Builds an RFC 9457 problem answer. */
export function problem(status: number, code: string, params?: Record<string, unknown>) {
  return HttpResponse.json(
    { type: 'about:blank', title: code, status, code, params },
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
  /** Sites and quick connections with connections open. */
  connected?: string[]
  /** Bookmarks every saved site has (4.14b). */
  bookmarks?: Bookmark[]
  /** Catalog of connectors; the built-in ones, both offered, by default. */
  connectors?: Connector[]
}

/** A connector of the catalog as the server describes it, offered unless said otherwise. */
export function connector(type: string, name: string, enabled = true, maturity: Connector['maturity'] = 'stable'): Connector {
  return {
    type,
    name,
    icon: 'server',
    maturity,
    capabilities: { folders: true, permissions: true, setTimes: true, resume: true, rename: true, serverCopy: false },
    fields: [],
    enabledByDefault: true,
    enabled,
  }
}

/** The connectors Transferly is built with, both offered, with the schemas the server answers. */
export const builtinConnectors: Connector[] = catalog as Connector[]

/** Answers the catalog of connectors. */
export function mockConnectors(connectors: Connector[] = builtinConnectors): void {
  server.use(http.get('/api/v1/connectors', () => HttpResponse.json(connectors)))
}

/**
 * Answers the workspace, collection, site, quick connection, open connection and bookmark lists, whatever is not given empty, and the
 * catalog of connectors, the built-in ones unless given.
 */
export function mockWorkspaceData({
  workspaces = [personalWorkspace],
  collections = [],
  sites = [],
  quick = [],
  connected = [],
  bookmarks = [],
  connectors = builtinConnectors,
}: WorkspaceData = {}): void {
  mockVault()
  mockConnectors(connectors)
  server.use(
    http.get('/api/v1/workspaces', () => HttpResponse.json(workspaces)),
    http.get('/api/v1/workspaces/:id/collections', () => HttpResponse.json(collections)),
    http.get('/api/v1/workspaces/:id/sites', () => HttpResponse.json(sites)),
    http.get('/api/v1/workspaces/:id/quick-connections', () => HttpResponse.json(quick)),
    http.get('/api/v1/workspaces/:id/connections', () => HttpResponse.json({ siteIds: connected })),
    http.get('/api/v1/workspaces/:id/sites/:siteId/bookmarks', () => HttpResponse.json(bookmarks)),
    http.get('/api/v1/version', () => HttpResponse.json({ version: 'v0.5.0' })),
  )
  mockServerTools(noTools)
}

/** What a server that cannot run commands offers (ADR-0027). */
export const noTools: ServerTools = { copy: false, checksums: [], archive: [], extract: [], usage: false }

/** Answers what the servers of every connection can do on their own. */
export function mockServerTools(tools: ServerTools): void {
  server.use(http.get('/api/v1/workspaces/:id/sites/:siteId/tools', () => HttpResponse.json(tools)))
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
  server.use(http.get('/api/v1/me/security', () => HttpResponse.json({ totp: false, recoveryCodesLeft: 0, passkeys: [] })))
}
