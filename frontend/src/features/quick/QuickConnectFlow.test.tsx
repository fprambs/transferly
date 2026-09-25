import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { SitesSidebar } from '@/features/sites/SitesSidebar'
import type { QuickConnection } from '@/lib/api'
import { mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'
import { useDialogsStore } from '@/state/dialogs-store'

import { QuickConnectFlow } from './QuickConnectFlow'

/** The connection the mock server opens. */
const opened: QuickConnection = {
  id: 'q1',
  name: '198.51.100.7',
  protocol: 'sftp',
  host: '198.51.100.7',
  port: 22,
  username: 'deploy',
  authMethod: 'password',
  listTimezone: 'UTC',
  createdAt: '2026-01-01T00:00:00Z',
}

/** Renders the sidebar with the quick connect journey and waits for the workspace to load. */
async function renderFlow() {
  const user = userEvent.setup()
  await renderWithProviders(
    <>
      <SitesSidebar />
      <QuickConnectFlow />
    </>,
  )
  await screen.findByRole('button', { name: /Quick connect/ })
  return user
}

/** Opens the dialog from the sidebar button. */
async function openDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Quick connect/ }))
  return screen.findByRole('dialog')
}

/** Types host, username and password, and optionally a port, then submits. */
async function fillAndConnect(user: ReturnType<typeof userEvent.setup>, port = '') {
  await user.type(screen.getByLabelText('Host'), '198.51.100.7')
  if (port !== '') {
    await user.type(screen.getByLabelText('Port'), port)
  }
  await user.type(screen.getByLabelText('Username'), 'deploy')
  await user.type(screen.getByLabelText('Password'), 'hunter2hunter2')
  await user.click(screen.getByRole('button', { name: 'Connect' }))
}

/**
 * Covers the quick connect journey: {@link QuickConnectFlow} and the dialogs and prompt it composes.
 *
 * Scenarios:
 * - the sidebar button and the Ctrl+Shift+K shortcut open the dialog; other keys do not
 * - the form validates before calling the server and Cancel closes it
 * - connecting sends the values with the default or the typed port, closes the dialog and offers to keep the server
 * - "Not now" dismisses the offer
 * - a server key that is not known yet asks for trust, going back returns to the form and trusting connects
 * - a failing trust call and a changed or refused server show their message
 * - saving files the connection under a name and a collection and refreshes the tree
 * - a rejected save shows its message and Cancel leaves the offer for later
 * - nothing is sent while the workspace is not loaded
 */
describe('quick connect', () => {
  beforeEach(() => {
    useDialogsStore.setState({ quickConnectOpen: false })
    mockWorkspaceData({ collections: [{ id: 'c1', name: 'Work' }, { id: 'c2', name: 'Staging', parentId: 'c1' }] })
  })

  it('opens from the button and the shortcut only', async () => {
    const user = await renderFlow()

    expect(await openDialog(user)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await user.keyboard('{Control>}k{/Control}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await user.keyboard('{Control>}{Shift>}k{/Shift}{/Control}')
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })

  it('validates the form before calling the server', async () => {
    let calls = 0
    server.use(http.post('/api/v1/workspaces/:id/quick-connections', () => (calls++, HttpResponse.json(opened))))
    const user = await renderFlow()
    await openDialog(user)

    await user.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await screen.findByText('Enter the host.')).toBeInTheDocument()
    expect(screen.getByText('Enter the username.')).toBeInTheDocument()
    expect(screen.getByText('Enter the password.')).toBeInTheDocument()

    await user.type(screen.getByLabelText('Port'), '70000')
    await user.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await screen.findByText('Enter a port between 1 and 65535.')).toBeInTheDocument()
    expect(calls).toBe(0)
  })

  it('connects with the default port and offers to keep the server', async () => {
    let body: unknown
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json(opened, { status: 201 })
      }),
    )
    const user = await renderFlow()
    await openDialog(user)

    await fillAndConnect(user)

    expect(await screen.findByText('It works. Keep this server?')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Connected to 198.51.100.7 · not saved')
    expect(body).toEqual({
      protocol: 'sftp',
      host: '198.51.100.7',
      port: 22,
      username: 'deploy',
      authMethod: 'password',
      password: 'hunter2hunter2',
    })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: 'Not now' }))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('sends the port that was typed', async () => {
    let body: { port?: number } = {}
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', async ({ request }) => {
        body = (await request.json()) as { port?: number }
        return HttpResponse.json(opened, { status: 201 })
      }),
    )
    const user = await renderFlow()
    await openDialog(user)

    await fillAndConnect(user, '2222')

    await screen.findByRole('status')
    expect(body.port).toBe(2222)
  })

  it('asks to trust an unknown server key, and connects after trusting it', async () => {
    const seen: string[] = []
    let attempts = 0
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', () => {
        attempts++
        if (attempts <= 2) {
          return HttpResponse.json(
            { type: 'about:blank', title: 'x', status: 409, code: 'host_key_unknown', params: { keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc' } },
            { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
          )
        }
        seen.push('connected')
        return HttpResponse.json(opened, { status: 201 })
      }),
      http.post('/api/v1/workspaces/:id/known-hosts', async ({ request }) => {
        seen.push(JSON.stringify(await request.json()))
        return HttpResponse.json({}, { status: 201 })
      }),
    )
    const user = await renderFlow()
    await openDialog(user)
    await fillAndConnect(user)

    expect(await screen.findByText('Trust this server?')).toBeInTheDocument()
    expect(screen.getByText('SHA256:abc')).toBeInTheDocument()
    expect(screen.getByText('ssh-ed25519')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Back' }))
    expect(await screen.findByLabelText('Host')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Connect' }))
    await user.click(await screen.findByRole('button', { name: 'Trust and connect' }))

    expect(await screen.findByRole('status')).toBeInTheDocument()
    expect(seen).toEqual([
      JSON.stringify({ host: '198.51.100.7', port: 22, keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc' }),
      'connected',
    ])
  })

  it('shows the failure when trusting the key fails', async () => {
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', () =>
        HttpResponse.json(
          { type: 'about:blank', title: 'x', status: 409, code: 'host_key_unknown', params: { keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc' } },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
      http.post('/api/v1/workspaces/:id/known-hosts', () => problem(403, 'forbidden')),
    )
    const user = await renderFlow()
    await openDialog(user)
    await fillAndConnect(user)

    await user.click(await screen.findByRole('button', { name: 'Trust and connect' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong. Try again.')
  })

  it('treats a key without a fingerprint as an ordinary failure', async () => {
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', () =>
        HttpResponse.json(
          { type: 'about:blank', title: 'x', status: 409, code: 'host_key_unknown' },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    const user = await renderFlow()
    await openDialog(user)

    await fillAndConnect(user)

    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong. Try again.')
  })

  it.each([
    ['host_key_changed', 'The server key changed since it was trusted. This could be an attack: do not connect.'],
    ['remote_auth_failed', 'The server refused the username or password.'],
  ])('explains %s', async (code, message) => {
    server.use(http.post('/api/v1/workspaces/:id/quick-connections', () => problem(code === 'host_key_changed' ? 409 : 502, code)))
    const user = await renderFlow()
    await openDialog(user)

    await fillAndConnect(user)

    expect(await screen.findByRole('alert')).toHaveTextContent(message)
  })

  it('does nothing while the workspace is not loaded', async () => {
    mockWorkspaceData({ workspaces: [] })
    let calls = 0
    server.use(http.post('/api/v1/workspaces/:id/quick-connections', () => (calls++, HttpResponse.json(opened))))
    const user = await renderFlow()
    await openDialog(user)

    await fillAndConnect(user)

    expect(calls).toBe(0)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('saves the connection as a site in a collection', async () => {
    let saved: unknown
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', () => HttpResponse.json(opened, { status: 201 })),
      http.post('/api/v1/workspaces/:id/quick-connections/q1/save', async ({ request }) => {
        saved = await request.json()
        return HttpResponse.json({ id: 's1', name: 'Prod' }, { status: 201 })
      }),
    )
    const user = await renderFlow()
    await openDialog(user)
    await fillAndConnect(user)

    await user.click(await screen.findByRole('button', { name: 'Save as site…' }))
    const dialog = await screen.findByRole('dialog', { name: 'Save as site' })
    expect(within(dialog).getByLabelText('Name')).toHaveValue('198.51.100.7')
    expect(within(dialog).getByRole('option', { name: 'Work / Staging' })).toBeInTheDocument()

    await user.clear(within(dialog).getByLabelText('Name'))
    await user.type(within(dialog).getByLabelText('Name'), 'Prod')
    await user.selectOptions(within(dialog).getByLabelText('Collection'), 'c2')
    await user.click(within(dialog).getByRole('button', { name: 'Save site' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(saved).toEqual({ name: 'Prod', collectionId: 'c2' })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('saves at the top level when no collection is chosen', async () => {
    let saved: unknown
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', () => HttpResponse.json(opened, { status: 201 })),
      http.post('/api/v1/workspaces/:id/quick-connections/q1/save', async ({ request }) => {
        saved = await request.json()
        return HttpResponse.json({ id: 's1', name: 'x' }, { status: 201 })
      }),
    )
    const user = await renderFlow()
    await openDialog(user)
    await fillAndConnect(user)
    await user.click(await screen.findByRole('button', { name: 'Save as site…' }))

    await user.click(await screen.findByRole('button', { name: 'Save site' }))

    await waitFor(() => expect(saved).toEqual({ name: '198.51.100.7' }))
  })

  it('validates the name, shows a rejected save and keeps the offer when cancelled', async () => {
    server.use(
      http.post('/api/v1/workspaces/:id/quick-connections', () => HttpResponse.json(opened, { status: 201 })),
      http.post('/api/v1/workspaces/:id/quick-connections/q1/save', () => problem(409, 'site_name_taken')),
    )
    const user = await renderFlow()
    await openDialog(user)
    await fillAndConnect(user)
    await user.click(await screen.findByRole('button', { name: 'Save as site…' }))
    const dialog = await screen.findByRole('dialog', { name: 'Save as site' })

    await user.clear(within(dialog).getByLabelText('Name'))
    await user.click(within(dialog).getByRole('button', { name: 'Save site' }))
    expect(await within(dialog).findByText('Enter a name.')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Name'), 'Prod')
    await user.click(within(dialog).getByRole('button', { name: 'Save site' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('A site with this name already exists.')

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('closes the save dialog with Escape', async () => {
    server.use(http.post('/api/v1/workspaces/:id/quick-connections', () => HttpResponse.json(opened, { status: 201 })))
    const user = await renderFlow()
    await openDialog(user)
    await fillAndConnect(user)
    await user.click(await screen.findByRole('button', { name: 'Save as site…' }))
    await screen.findByRole('dialog', { name: 'Save as site' })

    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('closes the quick connect dialog with Escape', async () => {
    const user = await renderFlow()
    await openDialog(user)

    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('speaks the interface language', async () => {
    const user = userEvent.setup()
    await renderWithProviders(
      <>
        <SitesSidebar />
        <QuickConnectFlow />
      </>,
      'es',
    )

    await user.click(await screen.findByRole('button', { name: /Conexión rápida/ }))

    expect(await screen.findByRole('dialog', { name: 'Conexión rápida' })).toBeInTheDocument()
  })
})
