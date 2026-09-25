import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { adminSession, mockSession, mockSetupRequired, mockWorkspaceData, personalWorkspace, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

/**
 * Covers the route guards and the account screens through the whole application.
 *
 * Scenarios:
 * - a visitor without a session is sent to login, or to setup on a fresh install
 * - a signed-in user sees the shell and is kept away from login and setup
 * - setup is closed once the administrator exists
 * - signing in stores the session, sends the anti-CSRF token and opens the shell
 * - creating the administrator opens the shell
 * - a wrong password shows the server error and clears the password
 * - the form validates the email and the password before calling the server
 * - opening a site from the sidebar shows its folder, and moving through folders updates the address
 * - a site opened by its address shows nothing until the workspace is known
 * - the messages follow the interface language
 */
describe('routes', () => {
  beforeEach(() => {
    mockWorkspaceData()
  })

  it('sends a visitor to login when the install is complete', async () => {
    mockSession(null)
    mockSetupRequired(false)

    const { history } = await renderApp()

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/login')
  })

  it('sends a visitor to setup on a fresh install', async () => {
    mockSession(null)
    mockSetupRequired(true)

    const { history } = await renderApp()

    expect(await screen.findByRole('heading', { name: 'Create the administrator' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/setup')
  })

  it('shows the shell to a signed-in user', async () => {
    mockSession(adminSession)

    const { history } = await renderApp()

    expect(await screen.findByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/')
  })

  it.each(['/login', '/setup'])('keeps a signed-in user away from %s', async (path) => {
    mockSession(adminSession)

    const { history } = await renderApp({ path })

    expect(await screen.findByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/')
  })

  it('closes setup once the administrator exists', async () => {
    mockSession(null)
    mockSetupRequired(false)

    const { history } = await renderApp({ path: '/setup' })

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/login')
  })

  it('signs in, sends the anti-CSRF token afterwards and opens the shell', async () => {
    mockSession(null)
    mockSetupRequired(false)
    let sent: unknown
    server.use(
      http.post('/api/v1/auth/login', async ({ request }) => {
        sent = await request.json()
        return HttpResponse.json(adminSession)
      }),
    )
    let csrf: string | null = null
    server.use(
      http.post('/api/v1/auth/logout', ({ request }) => {
        csrf = request.headers.get('X-CSRF-Token')
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    const { history, transport } = await renderApp({ path: '/login' })

    await user.type(await screen.findByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password'), 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(sent).toEqual({ email: 'admin@example.com', password: 'correct horse battery' })
    expect(history.location.pathname).toBe('/')
    await transport.request({ method: 'POST', path: '/api/v1/auth/logout' })
    expect(csrf).toBe('csrf-token-1')
  })

  it('creates the administrator and opens the shell', async () => {
    mockSession(null)
    mockSetupRequired(true)
    server.use(http.post('/api/v1/setup', () => HttpResponse.json(adminSession, { status: 201 })))
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/setup' })

    await user.type(await screen.findByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password'), 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByRole('heading', { level: 1, name: 'Sentinel' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/')
  })

  it('shows the server error on a wrong password and clears the password', async () => {
    mockSession(null)
    mockSetupRequired(false)
    server.use(http.post('/api/v1/auth/login', () => problem(401, 'invalid_credentials')))
    const user = userEvent.setup()
    await renderApp({ path: '/login' })

    await user.type(await screen.findByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password'), 'wrong password')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The email or password is not right.')
    expect(screen.getByLabelText('Email')).toHaveValue('admin@example.com')
    expect(screen.getByLabelText('Password')).toHaveValue('')
  })

  it('validates login fields before calling the server', async () => {
    mockSession(null)
    mockSetupRequired(false)
    const user = userEvent.setup()
    await renderApp({ path: '/login' })

    await user.click(await screen.findByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('Enter your email.')).toBeInTheDocument()
    expect(screen.getByText('Enter your password.')).toBeInTheDocument()
    expect(screen.getByLabelText('Email')).toBeInvalid()

    await user.type(screen.getByLabelText('Email'), 'not-an-email')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('Enter a valid email address.')).toBeInTheDocument()
  })

  it('asks for a long password on setup and explains the rule', async () => {
    mockSession(null)
    mockSetupRequired(true)
    const user = userEvent.setup()
    await renderApp({ path: '/setup' })

    expect(await screen.findByText('At least 12 characters.')).toBeInTheDocument()
    await user.type(screen.getByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password'), 'short')
    await user.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByText('Use at least 12 characters.')).toBeInTheDocument()
  })

  it('opens a site from the sidebar and moves through its folders', async () => {
    mockSession(adminSession)
    mockWorkspaceData({
      sites: [
        {
          id: 's1',
          name: 'Prod',
          protocol: 'sftp',
          host: 'prod.example.com',
          port: 22,
          username: 'deploy',
          authMethod: 'password',
          listTimezone: 'UTC',
          secrets: [],
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    const entry = (name: string, type: 'file' | 'directory') => ({
      name,
      path: `/${name}`,
      type,
      size: 10,
      mode: '-rw-r--r--',
      modifiedAt: '2026-03-08T07:30:00Z',
      modifiedPrecision: 'minute',
    })
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', ({ request }) =>
        HttpResponse.json(new URL(request.url).searchParams.get('path') === '/' ? [entry('www', 'directory')] : [entry('index.html', 'file')]),
      ),
    )
    const user = userEvent.setup()
    const { history } = await renderApp()

    await user.click(await screen.findByRole('treeitem', { name: /Prod/ }))
    await user.dblClick(await screen.findByText('www'))

    expect(await screen.findByText('index.html')).toBeInTheDocument()
    expect(history.location.pathname).toBe('/sites/s1')
    expect(history.location.search).toBe('?path=%2Fwww')
  })

  it('waits for the workspace before showing a site opened by its address', async () => {
    mockSession(adminSession)
    mockWorkspaceData()
    server.use(
      http.get('/api/v1/workspaces', async () => {
        await delay(100)
        return HttpResponse.json([personalWorkspace])
      }),
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([])),
    )

    await renderApp({ path: '/sites/s1?path=%2Fwww' })

    await screen.findByRole('main', { name: 'Explorer' })
    expect(screen.queryByRole('navigation', { name: 'Path' })).not.toBeInTheDocument()
    expect(await screen.findByRole('navigation', { name: 'Path' })).toBeInTheDocument()
  })

  it('speaks the interface language', async () => {
    mockSession(null)
    mockSetupRequired(false)

    await renderApp({ path: '/login', language: 'es' })

    expect(await screen.findByRole('heading', { name: 'Inicia sesión' })).toBeInTheDocument()
  })
})
