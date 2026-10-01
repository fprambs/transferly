import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { KnownHost } from '@/lib/api'
import { findInnerDialog, queryInnerDialog } from '@/test/dialogs'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'
import { renderApp } from '@/test/render-app'

import { TrustedServers } from './TrustedServers'

/** A trusted server key. */
const key: KnownHost = { host: 'prod.example.com', port: 2222, keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc', firstSeen: '2026-03-08T07:30:00Z' }

/** Answers the list of known hosts. */
function mockHosts(hosts: KnownHost[]): void {
  server.use(http.get('/api/v1/workspaces/:id/known-hosts', () => HttpResponse.json(hosts)))
}

/**
 * Covers {@link TrustedServers}.
 *
 * Scenarios:
 * - the trusted servers are listed with key type, fingerprint and the date in the account's time zone
 * - without a session the dates use the browser's time zone
 * - nothing is shown as a list while loading, an empty workspace says so, and a failed load says so too
 * - forgetting asks for confirmation, then calls the server with the key and its fingerprint and refreshes the list; a server with several
 *   keys lists each one and forgets only the one chosen (4.13b)
 * - a failure is explained, and the dialog can be cancelled
 * - the user menu opens the page
 * - an address that leads nowhere shows the not found screen with a way home
 */
describe('trusted servers', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  it('lists the trusted servers', async () => {
    mockHosts([key])
    await renderApp({ path: '/servers' })

    const table = await screen.findByRole('table')

    expect(within(table).getByText('prod.example.com:2222')).toBeInTheDocument()
    expect(within(table).getByText('ssh-ed25519')).toBeInTheDocument()
    expect(within(table).getByText('SHA256:abc')).toBeInTheDocument()
    expect(within(table).getByText('Mar 8, 2026, 7:30 AM')).toBeInTheDocument()
  })

  it('uses the time zone of the browser without a session', async () => {
    mockSession(null)
    mockHosts([key])
    await renderWithProviders(<TrustedServers />)

    expect(await screen.findByText(/Mar 8, 2026/)).toBeInTheDocument()
  })

  it('says when no server is trusted, after a loading state', async () => {
    mockHosts([])
    server.use(
      http.get('/api/v1/workspaces/:id/known-hosts', async () => {
        await delay(100)
        return HttpResponse.json([])
      }),
    )
    await renderApp({ path: '/servers' })

    expect(await screen.findByText('Loading…')).toBeInTheDocument()
    expect(await screen.findByText('No server has been trusted yet.')).toBeInTheDocument()
  })

  it('says when the list could not be loaded', async () => {
    server.use(http.get('/api/v1/workspaces/:id/known-hosts', () => problem(500, 'internal')))
    await renderApp({ path: '/servers' })

    expect(await screen.findByRole('alert')).toHaveTextContent('The trusted servers could not be loaded.')
  })

  it('forgets a key after confirmation', async () => {
    const other: KnownHost = { ...key, fingerprint: 'SHA256:def' }
    let hosts = [key, other]
    let forgotten = ''
    server.use(
      http.get('/api/v1/workspaces/:id/known-hosts', () => HttpResponse.json(hosts)),
      http.delete('/api/v1/workspaces/:id/known-hosts', ({ request }) => {
        forgotten = new URL(request.url).search
        hosts = hosts.filter((host) => host.fingerprint !== new URL(request.url).searchParams.get('fingerprint'))
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    await renderApp({ path: '/servers' })

    await user.click(await screen.findByRole('button', { name: 'Forget prod.example.com:2222 SHA256:abc' }))
    const dialog = await screen.findByRole('dialog', { name: 'Forget this key?' })
    expect(dialog).toHaveTextContent('SHA256:abc')
    await user.click(within(dialog).getByRole('button', { name: 'Forget' }))

    await waitFor(() => expect(screen.queryByText('SHA256:abc')).not.toBeInTheDocument())
    expect(screen.getByText('SHA256:def')).toBeInTheDocument()
    expect(forgotten).toBe('?host=prod.example.com&port=2222&keyType=ssh-ed25519&fingerprint=SHA256%3Aabc')
  })

  it.each([
    ['forbidden', 403, 'You do not have permission to change this workspace.'],
    ['host_key_not_found', 404, 'This server key is already gone.'],
    ['weird', 500, 'Something went wrong. Try again.'],
  ])('explains %s and can be cancelled', async (code, status, message) => {
    mockHosts([key])
    server.use(http.delete('/api/v1/workspaces/:id/known-hosts', () => problem(status, code)))
    const user = userEvent.setup()
    await renderApp({ path: '/servers' })

    await user.click(await screen.findByRole('button', { name: 'Forget prod.example.com:2222 SHA256:abc' }))
    const dialog = await findInnerDialog()
    await user.click(within(dialog).getByRole('button', { name: 'Forget' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(message)

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(queryInnerDialog()).not.toBeInTheDocument())
  })

  it('opens from the settings of the user menu', async () => {
    mockHosts([])
    const user = userEvent.setup()
    const { history } = await renderApp()

    await user.click(await screen.findByRole('button', { name: 'Account' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Settings' }))
    await user.click(within(await screen.findByRole('navigation', { name: 'Settings' })).getByRole('button', { name: 'Trusted servers' }))

    expect(await screen.findByRole('heading', { level: 1, name: 'Trusted servers' })).toBeInTheDocument()
    expect(history.location.search).toContain('settings=servers')
  })

  it('shows the not found screen for an unknown address and leads home', async () => {
    mockHosts([])
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/no/such/page' })

    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Go to the start' }))

    expect(await screen.findByRole('heading', { name: 'Pick a site to start' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/')
  })
})
