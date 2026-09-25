import { http, HttpResponse } from 'msw'

import type { SessionInfo } from '@/lib/api'
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
