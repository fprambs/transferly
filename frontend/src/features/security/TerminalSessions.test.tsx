import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { CliSession } from '@/lib/api'
import { findInnerDialog, queryInnerDialog } from '@/test/dialogs'
import { adminSession, mockSecurity, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

/** A session of the terminal client signed in from a laptop. */
const laptop: CliSession = {
  id: 's-laptop',
  name: 'transferly-tui',
  host: 'laptop',
  ip: '10.0.0.9',
  userAgent: 'transferly-tui/1.4.0',
  createdAt: '2026-01-01T12:00:00Z',
  lastSeenAt: '2026-01-02T08:30:00Z',
  expiresAt: '2026-01-31T12:00:00Z',
  current: false,
}

/** Answers the sessions of the terminal client from a list the revocations change, and records what was revoked. */
function mockSessions(sessions: CliSession[]): string[] {
  const revoked: string[] = []
  let list = sessions
  server.use(
    http.get('/api/v1/me/sessions', () => HttpResponse.json(list)),
    http.delete('/api/v1/me/sessions/:id', ({ params }) => {
      revoked.push(String(params.id))
      list = list.filter((session) => session.id !== params.id)
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return revoked
}

/**
 * Covers the sessions of the terminal client in the security settings (ADR-0029).
 *
 * Scenarios:
 * - without sessions the section says how to sign in a terminal
 * - when the sessions cannot be read the section says so instead of showing none
 * - each session shows its client and computer, when it started, when it was last used and from where; one without a host shows its
 *   name only; the session making the request is marked
 * - ending a session asks first, can be cancelled, and once confirmed the session leaves the list
 * - a failure to end it explains itself in the dialog
 * - the section speaks Spanish
 */
describe('TerminalSessions', () => {
  beforeEach(() => {
    mockWorkspaceData()
    mockSession(adminSession)
    mockSecurity()
  })

  it('says how to sign in a terminal when there is none', async () => {
    await renderApp({ path: '/?settings=security' })
    expect(await screen.findByText('No terminal is signed in. Install transferly-tui and sign in from it.')).toBeInTheDocument()
  })

  it('says when the sessions cannot be read instead of showing none', async () => {
    server.use(http.get('/api/v1/me/sessions', () => problem(500, 'internal_error')))
    await renderApp({ path: '/?settings=security' })

    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.queryByText('No terminal is signed in. Install transferly-tui and sign in from it.')).not.toBeInTheDocument()
  })

  it('lists the sessions with their computer, times and address', async () => {
    mockSessions([laptop, { ...laptop, id: 's-ci', host: '', current: true }])
    await renderApp({ path: '/?settings=security' })

    const list = await screen.findByRole('list', { name: 'Terminal sessions' })
    const rows = within(list).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('transferly-tui on laptop')
    expect(rows[0]).toHaveTextContent(/Signed in .*2026.* · last used .*2026.* · from 10\.0\.0\.9/)
    expect(within(rows[0]).queryByText('This terminal')).not.toBeInTheDocument()
    expect(rows[1]).toHaveTextContent(/^transferly-tuiSigned in/)
    expect(within(rows[1]).getByText('This terminal')).toBeInTheDocument()
  })

  it('ends a session after asking', async () => {
    const revoked = mockSessions([laptop])
    const user = userEvent.setup()
    await renderApp({ path: '/?settings=security' })

    await user.click(await screen.findByRole('button', { name: 'End the session of transferly-tui on laptop' }))
    let dialog = await findInnerDialog()
    expect(within(dialog).getByRole('heading', { name: 'End the session of transferly-tui on laptop?' })).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(queryInnerDialog()).toBeNull())
    expect(revoked).toEqual([])

    await user.click(screen.getByRole('button', { name: 'End the session of transferly-tui on laptop' }))
    dialog = await findInnerDialog()
    await user.click(within(dialog).getByRole('button', { name: 'End session' }))

    expect(await screen.findByText('No terminal is signed in. Install transferly-tui and sign in from it.')).toBeInTheDocument()
    expect(revoked).toEqual(['s-laptop'])
  })

  it('explains a failure to end a session', async () => {
    mockSessions([laptop])
    server.use(http.delete('/api/v1/me/sessions/:id', () => problem(404, 'session_not_found')))
    const user = userEvent.setup()
    await renderApp({ path: '/?settings=security' })

    await user.click(await screen.findByRole('button', { name: 'End the session of transferly-tui on laptop' }))
    const dialog = await findInnerDialog()
    await user.click(within(dialog).getByRole('button', { name: 'End session' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('That session already ended.')
    expect(within(dialog).getByText('Code: session_not_found')).toBeInTheDocument()
  })

  it('speaks Spanish', async () => {
    mockSessions([laptop])
    mockSession({ ...adminSession, user: { ...adminSession.user, locale: 'es' } })
    await renderApp({ path: '/?settings=security', language: 'es' })
    expect(await screen.findByRole('button', { name: 'Cerrar la sesión de transferly-tui en laptop' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Sesiones de terminal' })).toBeInTheDocument()
  })
})
