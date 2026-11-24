import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { adminSession, mockSession, mockSetupRequired, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

import { leaveTo } from './request'

vi.mock('./request', async (original) => ({ ...(await original<typeof import('./request')>()), leaveTo: vi.fn() }))

/** A challenge of the right shape. */
const challenge = 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM'

/** The address of the approval page for a request with the given extra parameters. */
function page(extra: Record<string, string> = {}): string {
  const params = new URLSearchParams({ code_challenge: challenge, code_challenge_method: 'S256', client_name: 'transferly-tui', client_host: 'laptop', state: 'st', ...extra })
  return `/cli/authorize?${params.toString()}`
}

/** Records the approvals the server receives and answers them with the given reply. */
function mockApproval(reply: () => Response): unknown[] {
  const received: unknown[] = []
  server.use(
    http.post('/api/v1/cli/authorizations', async ({ request }) => {
      received.push(await request.json())
      return reply()
    }),
  )
  return received
}

/**
 * Covers the page where the person approves a sign-in of the terminal client (ADR-0029).
 *
 * Scenarios:
 * - a malformed request, or one that would send the answer off this computer, is not offered for approval
 * - the page says which client on which computer asks for which account; without a host it names the client only
 * - approving needs the password, sends the request with the trimmed names, and hands the code back to the terminal with the state
 * - without a redirect the code is shown to paste, with a warning and a button that copies it
 * - a wrong password explains itself, with the guidance of its code, and clears the field
 * - denying sends the refusal back to the terminal, or says the terminal was not signed in when there is nowhere to send it
 * - a visitor signs in first and comes back to the same request; the page speaks Spanish
 */
describe('CliAuthorizePage', () => {
  beforeEach(() => {
    vi.mocked(leaveTo).mockClear()
    mockWorkspaceData()
    mockSession(adminSession)
  })

  it.each([page({ code_challenge: 'short' }), page({ redirect_uri: 'https://evil.example.com/callback' }), '/cli/authorize'])('refuses the request %s', async (path) => {
    await renderApp({ path })
    expect(await screen.findByRole('heading', { name: 'This sign-in link is not valid' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument()
  })

  it('approves and hands the code back to the terminal', async () => {
    const received = mockApproval(() => HttpResponse.json({ code: 'the-code' }, { status: 201 }))
    const user = userEvent.setup()
    await renderApp({ path: page({ redirect_uri: 'http://127.0.0.1:49152/callback', client_name: ' transferly-tui ' }) })

    expect(await screen.findByText(`transferly-tui on laptop asks to use your account ${adminSession.user.email}.`)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Approve' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Enter your password.')
    expect(received).toHaveLength(0)

    await user.type(screen.getByLabelText('Your password'), 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Approve' }))

    expect(await screen.findByText('Going back to your terminal. You can close this page.')).toBeInTheDocument()
    expect(received).toEqual([
      { codeChallenge: challenge, redirectUri: 'http://127.0.0.1:49152/callback', clientName: 'transferly-tui', clientHost: 'laptop', password: 'correct horse battery' },
    ])
    expect(leaveTo).toHaveBeenCalledWith('http://127.0.0.1:49152/callback?code=the-code&state=st')
  })

  it('shows the code to paste when there is no redirect', async () => {
    mockApproval(() => HttpResponse.json({ code: 'paste-me' }, { status: 201 }))
    const user = userEvent.setup()
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    await renderApp({ path: page({ redirect_uri: '' }).replace('&client_host=laptop', '') })

    expect(await screen.findByText(`transferly-tui asks to use your account ${adminSession.user.email}.`)).toBeInTheDocument()
    await user.type(screen.getByLabelText('Your password'), 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Approve' }))

    expect(await screen.findByLabelText('Sign-in code')).toHaveTextContent('paste-me')
    expect(screen.getByText(/Never give this code to anyone/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Copy code' }))
    expect(writeText).toHaveBeenCalledWith('paste-me')
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(leaveTo).not.toHaveBeenCalled()
  })

  it('explains a wrong password and clears it', async () => {
    mockApproval(() => problem(403, 'invalid_password'))
    const user = userEvent.setup()
    await renderApp({ path: page() })

    await user.type(await screen.findByLabelText('Your password'), 'wrong password!!')
    await user.click(screen.getByRole('button', { name: 'Approve' }))

    expect(await screen.findByText('The password is not right.')).toBeInTheDocument()
    expect(screen.getByText('Code: invalid_password')).toBeInTheDocument()
    expect(screen.getByLabelText('Your password')).toHaveValue('')
  })

  it('sends the refusal back to the terminal', async () => {
    const user = userEvent.setup()
    await renderApp({ path: page({ redirect_uri: 'http://[::1]:5000/callback' }) })

    await user.click(await screen.findByRole('button', { name: 'Deny' }))

    expect(await screen.findByRole('heading', { name: 'Sign-in denied' })).toBeInTheDocument()
    expect(leaveTo).toHaveBeenCalledWith('http://[::1]:5000/callback?error=access_denied&state=st')
  })

  it('says the terminal was not signed in when there is nowhere to send the refusal', async () => {
    const user = userEvent.setup()
    await renderApp({ path: page() })

    await user.click(await screen.findByRole('button', { name: 'Deny' }))

    expect(await screen.findByText('The terminal was not signed in. You can close this page.')).toBeInTheDocument()
    expect(leaveTo).not.toHaveBeenCalled()
  })

  it('signs a visitor in and comes back to the same request', async () => {
    mockSession(null)
    mockSetupRequired(false)
    server.use(http.post('/api/v1/auth/login', () => HttpResponse.json(adminSession)))
    const user = userEvent.setup()
    const { history } = await renderApp({ path: page() })

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/login')
    await user.type(screen.getByLabelText('Email'), adminSession.user.email)
    await user.type(screen.getByLabelText('Password'), 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('heading', { name: 'Sign in from the terminal?' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/cli/authorize')
    expect(history.location.href).toContain('state=st')
  })

  it('speaks Spanish', async () => {
    await renderApp({ path: page(), language: 'es' })
    expect(await screen.findByRole('heading', { name: '¿Iniciar sesión desde la terminal?' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Aprobar' })).toBeInTheDocument()
  })
})
