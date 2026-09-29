import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { AuditEvent } from '@/lib/api'
import { useUiStore } from '@/state/ui-store'
import { adminSession, mockSession, mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose } from '@/test/select'
import { renderApp } from '@/test/render-app'
import { mockTeam, teamId } from '@/test/teams'

/** Builds an event. */
function event(id: number, action: string, target: string, extra: Partial<AuditEvent> = {}): AuditEvent {
  return { id, action, target, meta: {}, at: '2026-09-26T12:00:00Z', actor: 'maria@acme.dev', ip: '203.0.113.5', ...extra }
}

/** Answers the audit trail of the team with a function of the query, and records the queries. */
function mockAudit(answer: (query: URLSearchParams) => AuditEvent[]): URLSearchParams[] {
  const seen: URLSearchParams[] = []
  server.use(
    http.get(`/api/v1/workspaces/${teamId}/audit`, ({ request }) => {
      const query = new URL(request.url).searchParams
      seen.push(query)
      return HttpResponse.json(answer(query))
    }),
  )
  return seen
}

/**
 * Covers {@link AuditPage}.
 *
 * Scenarios:
 * - every action of the trail is worded as a sentence with its target and facts, an unknown action gets the generic sentence, and an event
 *   whose account was deleted says so
 * - the group filter narrows the trail by the prefix of its actions and back to everything
 * - a full page offers to load more and reads the next page from the last event
 * - an empty trail and a slow one are explained
 * - personal workspaces and members who cannot manage the workspace get a message instead of the trail
 * - the switcher offers the audit log to admins and the owner and opens it
 */
describe('audit page', () => {
  beforeEach(() => {
    useUiStore.setState({ workspaceId: teamId })
  })

  it('words every action', async () => {
    mockTeam()
    mockAudit(() => [
      event(15, 'workspace.created', 'Acme Web'),
      event(14, 'workspace.ownership_transferred', 'maria@acme.dev'),
      event(13, 'member.joined', 'ana@acme.dev', { meta: { role: 'editor' } }),
      event(12, 'member.role_changed', 'jorge@acme.dev', { meta: { role: 'admin' } }),
      event(11, 'member.removed', 'pablo@acme.dev'),
      event(10, 'member.left', 'lucia@acme.dev'),
      event(9, 'invitation.created', 'bob@acme.dev'),
      event(8, 'invitation.revoked', 'bob@acme.dev'),
      event(7, 'key.rotated', '', { meta: { version: '3' } }),
      event(6, 'site.created', 'Production'),
      event(5, 'site.updated', 'Production'),
      event(4, 'site.deleted', 'Old'),
      event(3, 'sites.imported', 'filezilla', { meta: { sites: '12' } }),
      event(2, 'host_key.trusted', 'files.example.com:22'),
      event(1, 'host_key.forgotten', 'files.example.com:22', { actor: undefined, ip: undefined }),
      event(0, 'thing.happened', 'x'),
    ])
    await renderApp({ path: '/audit' })

    const list = await screen.findByRole('list', { name: 'Audit log' })
    const text = list.textContent ?? ''
    for (const sentence of [
      'created the workspace Acme Web',
      'handed the workspace to maria@acme.dev',
      'ana@acme.dev joined as Editor',
      'made jorge@acme.dev Admin',
      'removed pablo@acme.dev',
      'lucia@acme.dev left',
      'invited bob@acme.dev as Viewer',
      'revoked the invitation of bob@acme.dev',
      'rotated the workspace key (version 3)',
      'created the site Production',
      'changed the site Production',
      'deleted the site Old',
      'imported 12 sites',
      'trusted the server files.example.com:22',
      'forgot the server files.example.com:22',
      'thing.happened x',
      'Deleted account',
    ]) {
      expect(text, sentence).toContain(sentence)
    }
    expect(within(list).getAllByRole('listitem')).toHaveLength(16)
  })

  it('narrows the trail by group', async () => {
    mockTeam()
    const seen = mockAudit((query) => (query.get('action') === 'member.' ? [event(2, 'member.left', 'a@acme.dev')] : [event(1, 'site.created', 'Prod')]))
    const user = userEvent.setup()
    await renderApp({ path: '/audit' })

    expect(await screen.findByText('created the site Prod')).toBeInTheDocument()
    await choose(user, screen.getByLabelText('Show'), 'Members')
    expect(await screen.findByText('a@acme.dev left')).toBeInTheDocument()
    expect(screen.queryByText('created the site Prod')).not.toBeInTheDocument()
    await choose(user, screen.getByLabelText('Show'), 'Everything')

    expect(await screen.findByText('created the site Prod')).toBeInTheDocument()
    expect(seen.map((query) => query.get('action'))).toEqual([null, 'member.', null].slice(0, seen.length))
  })

  it('loads the next page from the last event', async () => {
    mockTeam()
    const page = Array.from({ length: 50 }, (_, index) => event(100 - index, 'site.created', `Site ${100 - index}`))
    const seen = mockAudit((query) => (query.get('before') === null ? page : [event(50, 'site.deleted', 'Last one')]))
    const user = userEvent.setup()
    await renderApp({ path: '/audit' })

    await screen.findByText('created the site Site 100')
    await user.click(screen.getByRole('button', { name: 'Load more' }))

    expect(await screen.findByText('deleted the site Last one')).toBeInTheDocument()
    expect(seen[1].get('before')).toBe('51')
    expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument()
  })

  it('says when nothing was recorded and while it loads', async () => {
    mockTeam()
    server.use(
      http.get(`/api/v1/workspaces/${teamId}/audit`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 150))
        return HttpResponse.json([])
      }),
    )
    await renderApp({ path: '/audit' })

    expect(await screen.findByText('Loading the audit log…')).toBeInTheDocument()
    expect(await screen.findByText('Nothing has been recorded yet.')).toBeInTheDocument()
  })

  it('explains a personal workspace and a role that cannot read the trail', async () => {
    mockSession(adminSession)
    mockWorkspaceData({ workspaces: [personalWorkspace] })
    useUiStore.setState({ workspaceId: null })
    await renderApp({ path: '/audit' })
    expect(await screen.findByText('Personal workspaces have no team audit log.')).toBeInTheDocument()
  })

  it('refuses an editor', async () => {
    mockTeam({ role: 'editor' })
    await renderApp({ path: '/audit' })

    expect(await screen.findByText('Only admins and the owner can read the audit log.')).toBeInTheDocument()
  })

  it('offers the audit log in the switcher to admins only', async () => {
    mockTeam({ role: 'admin' })
    mockAudit(() => [])
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })

    await user.click(await screen.findByRole('button', { name: 'Workspace' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Audit log' }))

    await waitFor(() => expect(history.location.pathname).toBe('/audit'))
  })

  it('does not offer the audit log to editors', async () => {
    mockTeam({ role: 'editor' })
    const user = userEvent.setup()
    await renderApp({ path: '/' })

    await user.click(await screen.findByRole('button', { name: 'Workspace' }))
    await screen.findByRole('menuitem', { name: 'Members' })
    expect(screen.queryByRole('menuitem', { name: 'Audit log' })).not.toBeInTheDocument()
  })
})
