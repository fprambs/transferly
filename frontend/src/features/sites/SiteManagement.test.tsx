import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'
import { mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { SitesSidebar } from './SitesSidebar'

/** A saved site. */
const prod: Site = {
  id: 's1',
  name: 'Prod',
  protocol: 'sftp',
  host: 'prod.example.com',
  port: 2222,
  username: 'deploy',
  authMethod: 'password',
  listTimezone: 'Europe/Madrid',
  charset: 'utf-8',
  secrets: ['password'],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** Answers with an unknown host key. */
function unknownKey() {
  return HttpResponse.json(
    { type: 'about:blank', title: 'x', status: 409, code: 'host_key_unknown', params: { keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc' } },
    { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
  )
}

/** Renders the sidebar and waits for the tree. */
async function renderSidebar() {
  const user = userEvent.setup()
  await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)
  await screen.findByRole('treeitem', { name: /Prod/ })
  return user
}

/** Opens the actions menu of a site or collection and picks an action. */
async function pick(user: ReturnType<typeof userEvent.setup>, item: string, action: string, label = 'Actions for') {
  await user.hover(screen.getByRole('treeitem', { name: new RegExp(item) }))
  await user.click(screen.getByRole('button', { name: `${label} ${item}` }))
  await user.click(await screen.findByRole('menuitem', { name: action }))
}

/**
 * Covers site management in the sidebar: {@link SiteDialog}, {@link ConnectionTest} and {@link DeleteSiteDialog}.
 *
 * Scenarios:
 * - a new site validates its fields, then is created with the typed values and the default port
 * - the character set of file names is offered for FTP and FTPS only, is UTF-8 by default and is sent with the site
 * - FTP and FTPS are offered next to SFTP, with their own default port; FTP warns about clear text, FTPS explains its ports, and neither offers keys
 * - key authentication swaps the password for the key fields
 * - a site can be started inside a collection from its menu
 * - a rejected save shows its message and Cancel closes the dialog
 * - editing prefills the site without secrets and saves the changes
 * - the connection test reports success, failures and asks to trust a new server key before testing again
 * - deleting asks for confirmation, reports failures and can be cancelled
 */
describe('site management', () => {
  beforeEach(() => {
    mockWorkspaceData({ sites: [prod], collections: [{ id: 'c1', name: 'Work' }] })
  })

  it('validates and creates a site', async () => {
    let body: unknown
    server.use(
      http.post('/api/v1/workspaces/:id/sites', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ ...prod, id: 's2' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })

    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    expect(await within(dialog).findByText('Enter a name.')).toBeInTheDocument()
    expect(within(dialog).getByText('Enter the host.')).toBeInTheDocument()
    expect(within(dialog).getByText('Enter the username.')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Name'), 'Staging')
    await user.type(within(dialog).getByLabelText('Host'), 'staging.example.com')
    await user.type(within(dialog).getByLabelText('Port'), '70000')
    await user.type(within(dialog).getByLabelText('Username'), 'deploy')
    await user.type(within(dialog).getByLabelText('Password'), 'secret')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    expect(await within(dialog).findByText('Enter a port between 1 and 65535.')).toBeInTheDocument()

    await user.clear(within(dialog).getByLabelText('Port'))
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(body).toEqual({
      name: 'Staging',
      protocol: 'sftp',
      host: 'staging.example.com',
      port: 22,
      username: 'deploy',
      authMethod: 'password',
      listTimezone: 'UTC',
      charset: 'utf-8',
      password: 'secret',
    })
  })

  it('creates FTP and FTPS sites with their own port and no key option', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/workspaces/:id/sites', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ ...prod, id: `s${bodies.length + 1}` }, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).queryByLabelText('Character set of file names')).not.toBeInTheDocument()
    expect(within(dialog).getAllByRole('option').map((option) => option.textContent)).toEqual(expect.arrayContaining(['SFTP', 'FTPS', 'FTP']))
    expect(within(dialog).getByLabelText('Port')).toHaveAttribute('placeholder', '22')

    await user.selectOptions(within(dialog).getByLabelText('Protocol'), 'ftp')
    expect(within(dialog).getByLabelText('Port')).toHaveAttribute('placeholder', '21')
    expect(within(dialog).getByRole('note')).toHaveTextContent('FTP sends the password and the files without encryption')
    expect(within(dialog).queryByLabelText('Sign in with')).not.toBeInTheDocument()

    expect(within(dialog).getByLabelText('Character set of file names')).toHaveValue('utf-8')
    await user.selectOptions(within(dialog).getByLabelText('Character set of file names'), 'windows-1252')
    await user.selectOptions(within(dialog).getByLabelText('Protocol'), 'ftps')
    expect(within(dialog).queryByRole('note')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/Port 990 starts with TLS/)).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Name', { exact: true }), 'Secure')
    await user.type(within(dialog).getByLabelText('Host'), 'ftp.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'u')
    await user.type(within(dialog).getByLabelText('Password'), 'pw')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toMatchObject({ protocol: 'ftps', port: 21, authMethod: 'password', password: 'pw', charset: 'windows-1252' })
  })

  it('swaps the password for the key fields', async () => {
    let body: Record<string, unknown> = {}
    server.use(
      http.post('/api/v1/workspaces/:id/sites', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...prod, id: 's2' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByLabelText('Password')).toBeInTheDocument()
    await user.selectOptions(within(dialog).getByLabelText('Sign in with'), 'key')
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Name'), 'Keyed')
    await user.type(within(dialog).getByLabelText('Host'), 'k.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'u')
    await user.type(within(dialog).getByLabelText('Private key'), 'KEY')
    await user.type(within(dialog).getByLabelText('Key passphrase'), 'phrase')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(body.privateKey).toBe('KEY'))
    expect(body).toMatchObject({ authMethod: 'key', passphrase: 'phrase' })
    expect(body.password).toBeUndefined()
  })

  it('starts a site inside a collection', async () => {
    const user = await renderSidebar()

    await pick(user, 'Work', 'New site here')

    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    expect(within(dialog).getByLabelText('Collection')).toHaveValue('c1')
  })

  it('shows a rejected save and can be cancelled', async () => {
    server.use(http.post('/api/v1/workspaces/:id/sites', () => problem(409, 'site_name_taken')))
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'Prod')
    await user.type(within(dialog).getByLabelText('Host'), 'h')
    await user.type(within(dialog).getByLabelText('Username'), 'u')

    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('A site with this name already exists.')

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('edits a site without showing its secrets', async () => {
    let body: Record<string, unknown> = {}
    server.use(
      http.put('/api/v1/workspaces/:id/sites/s1', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(prod)
      }),
    )
    const user = await renderSidebar()

    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog', { name: 'Edit site' })
    expect(within(dialog).getByLabelText('Name')).toHaveValue('Prod')
    expect(within(dialog).getByLabelText('Port')).toHaveValue('2222')
    expect(within(dialog).getByLabelText('Password')).toHaveValue('')
    expect(within(dialog).getByText('Leave blank to keep the stored value.')).toBeInTheDocument()

    await user.clear(within(dialog).getByLabelText('Name'))
    await user.type(within(dialog).getByLabelText('Name'), 'Production')
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(body).toMatchObject({ name: 'Production', port: 2222, listTimezone: 'Europe/Madrid' })
    expect(body.password).toBeUndefined()
  })

  it('tests the connection and reports success', async () => {
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([{ name: 'a' }, { name: 'b' }])))
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))

    expect(await within(dialog).findByRole('status')).toHaveTextContent('Connected. The server answered with 2 entries at the root.')
  })

  it('tests the connection and reports a failure', async () => {
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files', () => problem(502, 'remote_auth_failed')))
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))

    expect(await within(dialog).findByText('The server refused the credentials.')).toBeInTheDocument()
  })

  it('asks to trust a new key and tests again', async () => {
    let listed = 0
    let trusted: unknown
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => (++listed === 1 ? unknownKey() : HttpResponse.json([]))),
      http.post('/api/v1/workspaces/:id/known-hosts', async ({ request }) => {
        trusted = await request.json()
        return HttpResponse.json({}, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))
    expect(await within(dialog).findByText('SHA256:abc')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Trust and test again' }))

    expect(await within(dialog).findByRole('status')).toHaveTextContent('0 entries')
    expect(trusted).toEqual({ host: 'prod.example.com', port: 2222, keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc' })
  })

  it('reports a failure to trust the key', async () => {
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => unknownKey()),
      http.post('/api/v1/workspaces/:id/known-hosts', () => problem(403, 'forbidden')),
    )
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))
    await user.click(await within(dialog).findByRole('button', { name: 'Trust and test again' }))

    expect(await within(dialog).findByText('You do not have permission to change this workspace.')).toBeInTheDocument()
  })

  it('deletes a site after confirmation', async () => {
    let deleted = ''
    server.use(
      http.delete('/api/v1/workspaces/:id/sites/:siteId', ({ params }) => {
        deleted = String(params.siteId)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = await renderSidebar()

    await pick(user, 'Prod', 'Delete')
    const dialog = await screen.findByRole('dialog', { name: 'Delete this site?' })
    expect(dialog).toHaveTextContent('Prod and its stored credentials will be removed.')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(deleted).toBe('s1')
  })

  it('reports a failed delete and can be cancelled', async () => {
    server.use(http.delete('/api/v1/workspaces/:id/sites/:siteId', () => problem(404, 'site_not_found')))
    const user = await renderSidebar()

    await pick(user, 'Prod', 'Delete')
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('This site no longer exists.')

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
