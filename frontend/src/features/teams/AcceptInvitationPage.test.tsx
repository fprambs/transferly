import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { InvitationPreview, SessionInfo } from '@/lib/api'
import { useUiStore } from '@/state/ui-store'
import { adminSession, mockSession, mockSetupRequired, mockWorkspaceData, personalWorkspace, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'
import { teamId, teamWorkspace } from '@/test/teams'

/** What an invitation offers unless a test says otherwise. */
const offer: InvitationPreview = {
  workspaceName: 'Acme Web',
  email: 'ana@acme.dev',
  role: 'editor',
  invitedBy: 'felipe@acme.dev',
  expiresAt: '2026-10-03T12:00:00Z',
  accountExists: false,
}

/** The session of the invited person. */
const anaSession: SessionInfo = { user: { ...adminSession.user, id: 'u-ana', email: 'ana@acme.dev' }, csrfToken: 'csrf-ana' }

/** Answers the preview of the token `tok` with an offer, or with a problem. */
function mockPreview(answer: Partial<InvitationPreview> | Response = {}): { tokens: string[] } {
  const seen: string[] = []
  server.use(
    http.post('/api/v1/invitations/preview', async ({ request }) => {
      seen.push(((await request.json()) as { token: string }).token)
      return answer instanceof Response ? answer : HttpResponse.json({ ...offer, ...answer })
    }),
  )
  return { tokens: seen }
}

/** Answers the workspaces of somebody who joined the team. */
function mockJoinedWorkspaces(): void {
  mockWorkspaceData({ workspaces: [personalWorkspace, teamWorkspace('editor')] })
  server.use(http.get(`/api/v1/workspaces/${teamId}/members`, () => HttpResponse.json([])), http.get(`/api/v1/workspaces/${teamId}/invitations`, () => HttpResponse.json([])))
}

/**
 * Covers {@link AcceptInvitationPage}.
 *
 * Scenarios:
 * - a link without a token, an invalid or used invitation and a rate limit are explained the same way for every cause
 * - the token in the fragment is sent in the body of the preview, and the page names the workspace, who invited, the role and the expiry
 * - an address without an account chooses a password twice, is told when it is short or does not match, and joins as a signed-in member
 *   even when the workspace is not listed yet
 * - an address with an account signs in with its fixed email and joins, and a wrong password is explained
 * - somebody already signed in as the invited address joins with one click, and any failure is explained
 * - somebody signed in as another account is told so and can sign out
 */
describe('accept invitation page', () => {
  beforeEach(() => {
    useUiStore.setState({ workspaceId: null })
    mockSetupRequired(false)
  })

  it('explains a link without a token', async () => {
    mockSession(null)
    await renderApp({ path: '/invite' })

    expect(await screen.findByRole('heading', { name: 'This invitation cannot be used' })).toBeInTheDocument()
    expect(screen.getByText('The link has no invitation token.')).toBeInTheDocument()
  })

  it('explains an invitation that cannot be used without saying why', async () => {
    mockSession(null)
    mockPreview(problem(404, 'invitation_not_found'))
    await renderApp({ path: '/invite#gone' })

    expect(await screen.findByRole('alert')).toHaveTextContent('This invitation is not valid. It may be used, revoked, expired or meant for another address.')
  })

  it('explains a rate limit', async () => {
    mockSession(null)
    mockPreview(problem(429, 'rate_limited'))
    await renderApp({ path: '/invite#busy' })

    expect(await screen.findByRole('alert')).toHaveTextContent('Too many attempts.')
  })

  it('lets an address without an account choose a password and join', async () => {
    mockSession(null)
    const preview = mockPreview()
    let joined: unknown
    server.use(
      http.post('/api/v1/invitations/join', async ({ request }) => {
        joined = await request.json()
        return HttpResponse.json(anaSession, { status: 201 })
      }),
    )
    mockJoinedWorkspaces()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/invite#tok-abc' })

    expect(await screen.findByRole('heading', { name: 'You were invited to Acme Web' })).toBeInTheDocument()
    expect(preview.tokens).toEqual(['tok-abc'])
    expect(screen.getByText(/Invited by felipe@acme.dev\. You will join as Editor/)).toBeInTheDocument()
    expect(screen.getByLabelText('Email')).toHaveValue('ana@acme.dev')
    expect(screen.getByLabelText('Email')).toHaveAttribute('readonly')
    await user.click(screen.getByRole('button', { name: 'Create account and join' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Use at least 12 characters.')

    await user.type(screen.getByLabelText('Account password'), 'a long enough password')
    await user.type(screen.getByLabelText('Repeat password'), 'something else entirely')
    await user.click(screen.getByRole('button', { name: 'Create account and join' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The passwords do not match.')

    await user.clear(screen.getByLabelText('Repeat password'))
    await user.type(screen.getByLabelText('Repeat password'), 'a long enough password')
    await user.click(screen.getByRole('button', { name: 'Create account and join' }))

    await waitFor(() => expect(history.location.pathname).toBe('/members'))
    expect(joined).toEqual({ token: 'tok-abc', password: 'a long enough password' })
    expect(useUiStore.getState().workspaceId).toBe(teamId)
  })

  it('enters the members page even when the new workspace is not listed yet', async () => {
    mockSession(null)
    mockPreview()
    server.use(http.post('/api/v1/invitations/join', () => HttpResponse.json(anaSession, { status: 201 })))
    mockWorkspaceData({ workspaces: [personalWorkspace] })
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/invite#tok-abc' })

    await user.type(await screen.findByLabelText('Account password'), 'a long enough password')
    await user.type(screen.getByLabelText('Repeat password'), 'a long enough password')
    await user.click(screen.getByRole('button', { name: 'Create account and join' }))

    await waitFor(() => expect(history.location.pathname).toBe('/members'))
    expect(useUiStore.getState().workspaceId).toBeNull()
  })

  it('explains why joining failed', async () => {
    mockSession(null)
    mockPreview()
    server.use(http.post('/api/v1/invitations/join', () => problem(409, 'account_exists')))
    const user = userEvent.setup()
    await renderApp({ path: '/invite#tok-abc' })

    await user.type(await screen.findByLabelText('Account password'), 'a long enough password')
    await user.type(screen.getByLabelText('Repeat password'), 'a long enough password')
    await user.click(screen.getByRole('button', { name: 'Create account and join' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('This address already has an account. Sign in instead.')
  })

  it('signs an existing account in and joins', async () => {
    mockSession(null)
    mockPreview({ accountExists: true, invitedBy: undefined })
    let login: unknown
    let accepted: unknown
    server.use(
      http.post('/api/v1/auth/login', async ({ request }) => {
        login = await request.json()
        return HttpResponse.json(anaSession)
      }),
      http.post('/api/v1/invitations/accept', async ({ request }) => {
        accepted = await request.json()
        return HttpResponse.json(teamWorkspace('editor'))
      }),
    )
    mockJoinedWorkspaces()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/invite#tok-abc' })

    await user.type(await screen.findByLabelText('Account password'), 'my account password')
    await user.click(screen.getByRole('button', { name: 'Sign in and join' }))

    await waitFor(() => expect(history.location.pathname).toBe('/members'))
    expect(login).toEqual({ email: 'ana@acme.dev', password: 'my account password' })
    expect(accepted).toEqual({ token: 'tok-abc' })
    expect(useUiStore.getState().workspaceId).toBe(teamId)
  })

  it('explains a wrong password', async () => {
    mockSession(null)
    mockPreview({ accountExists: true })
    server.use(http.post('/api/v1/auth/login', () => problem(401, 'invalid_credentials')))
    const user = userEvent.setup()
    await renderApp({ path: '/invite#tok-abc' })

    await user.type(await screen.findByLabelText('Account password'), 'not the password')
    await user.click(screen.getByRole('button', { name: 'Sign in and join' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The email or password is wrong.')
    expect(screen.getByLabelText('Account password')).toHaveValue('')
  })

  it('joins with one click when signed in as the invited address', async () => {
    mockSession(anaSession)
    mockPreview({ accountExists: true })
    server.use(http.post('/api/v1/invitations/accept', () => HttpResponse.json(teamWorkspace('editor'))))
    mockJoinedWorkspaces()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/invite#tok-abc' })

    expect(await screen.findByText('You are signed in as ana@acme.dev.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Join Acme Web' }))

    await waitFor(() => expect(history.location.pathname).toBe('/members'))
  })

  it('explains why accepting failed', async () => {
    mockSession(anaSession)
    mockPreview({ accountExists: true })
    server.use(http.post('/api/v1/invitations/accept', () => problem(404, 'invitation_not_found')))
    const user = userEvent.setup()
    await renderApp({ path: '/invite#tok-abc' })

    await user.click(await screen.findByRole('button', { name: 'Join Acme Web' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('This invitation is not valid.')
  })

  it('tells somebody signed in as another account and lets them sign out', async () => {
    mockSession(adminSession)
    mockPreview({ accountExists: true })
    let out = false
    server.use(
      http.post('/api/v1/auth/logout', () => {
        out = true
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    await renderApp({ path: '/invite#tok-abc' })

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('This invitation is for ana@acme.dev, but you are signed in as admin@example.com.')
    await user.click(within(alert.parentElement as HTMLElement).getByRole('button', { name: 'Sign out' }))

    await waitFor(() => expect(out).toBe(true))
    expect(await screen.findByRole('button', { name: 'Sign in and join' })).toBeInTheDocument()
  })
})
