import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'
import { mockUnlocking, mockVault, mockWorkspaceData, problem } from '@/test/handlers'
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
  speedLimitBps: 0,
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
 * - what is needed to connect is on the first tab, the rest on Advanced, Transfer, Character set and Proxy and jump host, and a wrong field opens
 *   its tab and marks it
 * - the protocols are listed by their full names, SFTP offers a key file and FTP and FTPS the anonymous user; an anonymous site is saved with the
 *   anonymous user and no password
 * - the character set of file names is offered for FTP and FTPS only, is UTF-8 by default and is sent with the site
 * - FTP and FTPS are offered next to SFTP, with their own default port; FTP warns about clear text, FTPS explains its ports, and neither offers keys
 * - key authentication swaps the password for the key fields
 * - a site can be started inside a collection from its menu
 * - a proxy and a jump host can be configured, in both authentication methods, and hide again outside SFTP
 * - a rejected save shows its message and Cancel closes the dialog
 * - a locked vault is explained once a secret is typed, in place of a bare error
 * - editing prefills the site without secrets and saves the changes
 * - the connection test reports success, failures, asks to trust a new server key before testing again, and asks for the master password when the vault is locked
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
      speedLimitBps: 0,
      password: 'secret',
    })
  })

  it('keeps what is needed to connect on the first tab, and opens the tab of a field that is wrong', async () => {
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })

    expect(within(dialog).getByRole('tab', { name: 'General' })).toHaveAttribute('aria-selected', 'true')
    expect(within(dialog).getByLabelText('Host')).toBeVisible()
    expect(within(dialog).getByLabelText('Speed limit (MiB/s)')).not.toBeVisible()
    expect(within(dialog).getByLabelText('Proxy')).not.toBeVisible()

    await user.type(within(dialog).getByLabelText('Name'), 'Staging')
    await user.type(within(dialog).getByLabelText('Host'), 'staging.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'deploy')
    await user.click(within(dialog).getByRole('tab', { name: 'Transfer' }))
    expect(within(dialog).getByLabelText('Host')).not.toBeVisible()
    await user.type(within(dialog).getByLabelText('Speed limit (MiB/s)'), 'fast')
    await user.click(within(dialog).getByRole('tab', { name: 'General' }))
    expect(within(dialog).getByLabelText('Name')).toHaveValue('Staging')

    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    const transfer = within(dialog).getByRole('tab', { name: /Transfer/ })
    await waitFor(() => expect(transfer).toHaveAttribute('aria-selected', 'true'))
    expect(transfer).toHaveTextContent('(has errors)')
    expect(within(dialog).getByText('Enter a speed limit greater than 0, or leave it blank for none.')).toBeVisible()

    await user.click(within(dialog).getByRole('tab', { name: 'Proxy and jump host' }))
    expect(within(dialog).getByLabelText('Proxy')).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    await waitFor(() => expect(within(dialog).getByRole('tab', { name: /Proxy and jump host/ })).toHaveAttribute('aria-selected', 'false'))
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
    expect(within(within(dialog).getByLabelText('Protocol')).getAllByRole('option').map((option) => option.textContent)).toEqual([
      'SFTP - SSH File Transfer Protocol',
      'FTPS - FTP over TLS',
      'FTP - File Transfer Protocol (not encrypted)',
    ])
    expect(within(within(dialog).getByLabelText('Logon type')).getAllByRole('option').map((option) => option.textContent)).toEqual(['Normal', 'Key file'])
    expect(within(dialog).getByLabelText('Port')).toHaveAttribute('placeholder', '22')

    await user.selectOptions(within(dialog).getByLabelText('Protocol'), 'ftp')
    expect(within(dialog).getByLabelText('Port')).toHaveAttribute('placeholder', '21')
    expect(within(dialog).getByRole('note')).toHaveTextContent('FTP sends the password and the files without encryption')
    expect(within(within(dialog).getByLabelText('Logon type')).getAllByRole('option').map((option) => option.textContent)).toEqual(['Normal', 'Anonymous'])

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

  it('saves an anonymous FTP site with the anonymous user and no password', async () => {
    let body: Record<string, unknown> = {}
    server.use(
      http.post('/api/v1/workspaces/:id/sites', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...prod, id: 's3' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Name'), 'Public')
    await user.selectOptions(within(dialog).getByLabelText('Protocol'), 'ftp')
    await user.type(within(dialog).getByLabelText('Host'), 'ftp.example.com')
    await user.selectOptions(within(dialog).getByLabelText('Logon type'), 'anonymous')
    expect(within(dialog).getByLabelText('Username')).toHaveValue('anonymous')
    expect(within(dialog).getByLabelText('Username')).toBeDisabled()
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/Signs in as anonymous/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(body.username).toBe('anonymous'))
    expect(body).toMatchObject({ protocol: 'ftp', authMethod: 'password', password: '' })
  })

  it('says a stored key and its passphrase are kept when left blank', async () => {
    mockWorkspaceData({ sites: [{ ...prod, authMethod: 'key', secrets: ['private_key', 'passphrase'] }], collections: [] })
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog', { name: 'Edit site' })

    expect(within(dialog).getByLabelText('Logon type')).toHaveValue('key')
    expect(within(dialog).getByLabelText('Private key')).toHaveAttribute('placeholder', 'Leave blank to keep the existing key.')
    expect(within(dialog).getByLabelText('Key passphrase')).toHaveAttribute('placeholder', 'Leave blank to keep the existing passphrase.')
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
    await user.selectOptions(within(dialog).getByLabelText('Logon type'), 'key')
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

  it('configures a proxy and a jump host, in both authentication methods', async () => {
    let body: Record<string, unknown> = {}
    server.use(
      http.post('/api/v1/workspaces/:id/sites', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...prod, id: 's2' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    let dialog = await screen.findByRole('dialog')

    expect(within(dialog).queryByLabelText('Proxy host')).not.toBeInTheDocument()
    await user.selectOptions(within(dialog).getByLabelText('Proxy'), 'socks5')
    await user.type(within(dialog).getByLabelText('Proxy host'), 'proxy.example.com')
    await user.clear(within(dialog).getByLabelText('Proxy port'))
    await user.type(within(dialog).getByLabelText('Proxy port'), '1080')
    await user.type(within(dialog).getByLabelText('Proxy username'), 'proxyuser')
    await user.type(within(dialog).getByLabelText('Proxy password'), 'proxypass')

    expect(within(dialog).queryByLabelText('Jump username')).not.toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('Jump host'), 'bastion.example.com')
    await user.type(within(dialog).getByLabelText('Jump username'), 'bastion')
    await user.type(within(dialog).getByLabelText('Jump host password'), 'bastionpass')

    await user.type(within(dialog).getByLabelText('Name'), 'Jumped')
    await user.type(within(dialog).getByLabelText('Host'), 'h.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'u')
    await user.type(within(dialog).getByLabelText('Password'), 'pw')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    expect(body).toMatchObject({
      proxy: { kind: 'socks5', host: 'proxy.example.com', port: 1080, username: 'proxyuser' },
      proxyPassword: 'proxypass',
      jump: { host: 'bastion.example.com', port: 22, username: 'bastion', authMethod: 'password' },
      jumpPassword: 'bastionpass',
    })

    await user.click(screen.getByRole('button', { name: 'New site' }))
    dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Jump host'), 'bastion.example.com')
    await user.selectOptions(within(dialog).getByLabelText('Jump host logon type'), 'key')
    expect(within(dialog).queryByLabelText('Jump host password')).not.toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('Jump host private key'), 'KEY')
    await user.type(within(dialog).getByLabelText('Jump host key passphrase'), 'phrase')
    await user.type(within(dialog).getByLabelText('Jump username'), 'bastion')

    await user.type(within(dialog).getByLabelText('Name'), 'Jumped key')
    await user.type(within(dialog).getByLabelText('Host'), 'h.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'u')
    await user.type(within(dialog).getByLabelText('Password'), 'pw')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(body.jump).toMatchObject({ authMethod: 'key' }))
    expect(body).toMatchObject({ jumpPrivateKey: 'KEY', jumpPassphrase: 'phrase' })
    expect(body.jumpPassword).toBeUndefined()

    await user.click(screen.getByRole('button', { name: 'New site' }))
    dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('SSH jump host')).toBeInTheDocument()
    await user.selectOptions(within(dialog).getByLabelText('Protocol'), 'ftp')
    expect(within(dialog).queryByText('SSH jump host')).not.toBeInTheDocument()
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

  it('explains a locked vault once a secret is typed, instead of letting the save fail', async () => {
    mockVault({ initialized: true, unlocked: false })
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).queryByRole('status')).not.toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('Password'), 'secret')
    expect(await within(dialog).findByRole('status')).toBeInTheDocument()
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
    expect(within(dialog).getByLabelText('Password')).toHaveAttribute('placeholder', 'Leave blank to keep the existing password.')

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

  it('asks for the master password when the test finds the vault locked, and tests again once it is unlocked', async () => {
    const vault = mockUnlocking()
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files', () => (vault.unlocked() ? HttpResponse.json([]) : problem(409, 'vault_locked'))))
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog', { name: 'Edit site' })

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))
    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    expect(unlock).toHaveTextContent(prod.host)
    await user.click(within(unlock).getByRole('button', { name: 'Cancel' }))
    expect(await within(dialog).findByText('Unlock the vault to save credentials.')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))
    const again = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    await user.type(within(again).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(again).getByRole('button', { name: 'Unlock' }))
    expect(await within(dialog).findByText(/Connected. The server answered/)).toBeInTheDocument()
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
