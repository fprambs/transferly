import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'
import { mockUnlocking, mockVault, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose, optionsOf } from '@/test/select'
import { renderWithProviders } from '@/test/render'

import { SitesSidebar } from './SitesSidebar'

/** A saved site. */
/** Answers key inspections as for an encrypted Ed25519 key whose passphrase is "phrase". */
function mockKeyInspection(): void {
  server.use(
    http.post('/api/v1/ssh-keys/inspect', async ({ request }) => {
      const { passphrase } = (await request.json()) as { passphrase?: string }
      const status = passphrase === undefined ? 'passphrase_required' : passphrase === 'phrase' ? 'usable' : 'wrong_passphrase'
      return HttpResponse.json({ private: true, format: 'openssh', algorithm: 'ssh-ed25519', bits: 256, fingerprint: 'SHA256:abc', encrypted: true, status })
    }),
  )
}

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
 * - the default remote folder, on Advanced, must be absolute and is sent as typed (4.14i)
 * - an FTP site of the account logon type sends its password and account, and an edited one keeps its stored account (4.8e)
 * - what is needed to connect is on the first tab, the rest on Advanced, Transfer, Character set and Proxy and jump host, and a wrong field opens
 *   its tab and marks it
 * - the protocols are listed by their full names, SFTP offers a key file and FTP and FTPS the anonymous user; an anonymous site is saved with the
 *   anonymous user and no password
 * - the character set of file names is offered for FTP and FTPS only, is UTF-8 by default and is sent with the site
 * - FTP is offered next to SFTP with the four encryptions of FileZilla, requiring explicit TLS by default; each encryption explains itself, clear
 *   text and the optional fall back warn, implicit TLS defaults to port 990, and FTP offers no key
 * - key authentication swaps the password for a key file, whose passphrase is asked once the server reads the key as encrypted; a stored key
 *   is kept unless another file is chosen
 * - a private key the server refuses on save is explained, and the form opens the tab of that key, the site's or the jump host's
 * - a site can be started inside a collection from its menu
 * - a proxy and a jump host can be configured, in both authentication methods, and hide again outside SFTP
 * - a rejected save shows its message and Cancel closes the dialog
 * - a locked vault is explained once a secret is typed, in place of a bare error
 * - editing prefills the site without secrets and saves the changes
 * - the connection test reports success, failures, asks to trust a new server key before testing again, asks for the master password when the vault is locked, and
 *   for the site's password when it asks for one
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
      remoteDir: '',
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

  it('saves an FTP site of the account logon type with its password and account', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/workspaces/:id/sites', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ ...prod, id: 's9' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    await user.type(within(dialog).getByLabelText('Name'), 'Billed')
    await choose(user, within(dialog).getByLabelText('Protocol'), 'FTP - File Transfer Protocol')
    await user.type(within(dialog).getByLabelText('Host'), 'ftp.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'u')
    expect(within(dialog).queryByLabelText('Account')).not.toBeInTheDocument()
    await choose(user, within(dialog).getByLabelText('Logon type'), 'Account')
    await user.type(within(dialog).getByLabelText('Password'), 'pw')
    await user.type(within(dialog).getByLabelText('Account'), 'billing')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(bodies[0]).toMatchObject({ authMethod: 'account', password: 'pw', account: 'billing' }))
  })

  it('keeps the stored account of a site unless another is typed', async () => {
    mockWorkspaceData({ sites: [{ ...prod, protocol: 'ftp', authMethod: 'account', secrets: ['password', 'account'] }], collections: [] })
    const user = await renderSidebar()
    await user.click(within(await screen.findByRole('treeitem', { name: /Prod/ })).getByRole('button', { name: 'Actions for Prod' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', { name: 'Edit site' })

    expect(within(dialog).getByLabelText('Logon type')).toHaveTextContent('Account')
    expect(within(dialog).getByLabelText('Account')).toHaveAttribute('placeholder', 'Leave blank to keep the existing account.')
  })

  it('asks for an absolute default remote folder on the Advanced tab and sends it', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/workspaces/:id/sites', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ ...prod, id: 's9' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    await user.type(within(dialog).getByLabelText('Name'), 'Web')
    await user.type(within(dialog).getByLabelText('Host'), 'web.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'deploy')
    await user.click(within(dialog).getByRole('tab', { name: 'Advanced' }))
    await user.type(within(dialog).getByLabelText('Default remote folder'), 'var/www')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    expect(await within(dialog).findByText('The folder must start with /.')).toBeVisible()

    await user.clear(within(dialog).getByLabelText('Default remote folder'))
    await user.type(within(dialog).getByLabelText('Default remote folder'), '/var/www')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    await waitFor(() => expect(bodies[0]).toMatchObject({ remoteDir: '/var/www' }))
  })

  it('creates FTP sites with the encryption FileZilla offers, their own port and no key option', async () => {
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
    expect(within(dialog).queryByLabelText('Encryption')).not.toBeInTheDocument()
    expect(await optionsOf(user, within(dialog).getByLabelText('Protocol'))).toEqual([
      'SFTP - SSH File Transfer Protocol',
      'FTP - File Transfer Protocol',
    ])
    expect(await optionsOf(user, within(dialog).getByLabelText('Logon type'))).toEqual(['Normal', 'Ask for password', 'Interactive', 'Key file', 'Anonymous'])
    expect(within(dialog).getByLabelText('Port')).toHaveAttribute('placeholder', '22')

    await choose(user, within(dialog).getByLabelText('Protocol'), 'FTP - File Transfer Protocol')
    const encryption = within(dialog).getByLabelText('Encryption')
    expect(await optionsOf(user, encryption)).toEqual([
      'Use explicit FTP over TLS if available',
      'Require explicit FTP over TLS',
      'Require implicit FTP over TLS',
      'Only use plain FTP (insecure)',
    ])
    expect(encryption).toHaveTextContent('Require explicit FTP over TLS')
    expect(within(dialog).queryByRole('note')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/upgrades with AUTH TLS before signing in/)).toBeInTheDocument()
    expect(within(dialog).getByLabelText('Port')).toHaveAttribute('placeholder', '21')
    expect(await optionsOf(user, within(dialog).getByLabelText('Logon type'))).toEqual(['Normal', 'Ask for password', 'Interactive', 'Account', 'Anonymous'])

    await choose(user, encryption, 'Require implicit FTP over TLS')
    expect(within(dialog).getByLabelText('Port')).toHaveAttribute('placeholder', '990')
    await choose(user, encryption, 'Use explicit FTP over TLS if available')
    expect(within(dialog).getByRole('note')).toHaveTextContent('someone on the network removes the offer')
    await choose(user, encryption, 'Only use plain FTP (insecure)')
    expect(within(dialog).getByRole('note')).toHaveTextContent('FTP sends the password and the files without encryption')

    expect(within(dialog).getByLabelText('Character set of file names')).toHaveTextContent('UTF-8')
    await choose(user, within(dialog).getByLabelText('Character set of file names'), 'Windows-1252 (Western European)')
    await choose(user, encryption, 'Require implicit FTP over TLS')
    await user.type(within(dialog).getByLabelText('Name', { exact: true }), 'Secure')
    await user.type(within(dialog).getByLabelText('Host'), 'ftp.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'u')
    await user.type(within(dialog).getByLabelText('Password'), 'pw')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toMatchObject({ protocol: 'ftp', ftpEncryption: 'implicit', port: 990, authMethod: 'password', password: 'pw', charset: 'windows-1252' })
  })

  it('saves an anonymous SFTP site with the anonymous user and no secret', async () => {
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
    await user.type(within(dialog).getByLabelText('Host'), 'sftp.example.com')
    await choose(user, within(dialog).getByLabelText('Logon type'), 'Anonymous')
    expect(within(dialog).getByLabelText('Username')).toHaveValue('anonymous')
    expect(within(dialog).getByLabelText('Username')).toBeDisabled()
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/Signs in as anonymous/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(body.username).toBe('anonymous'))
    expect(body).toMatchObject({ protocol: 'sftp', authMethod: 'anonymous' })
    expect(body).not.toHaveProperty('password')
    expect(body).not.toHaveProperty('privateKey')
  })

  it('saves an interactive site with its user and no secret', async () => {
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

    await user.type(within(dialog).getByLabelText('Name'), 'Bastion')
    await user.type(within(dialog).getByLabelText('Host'), 'bastion.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'deploy')
    await choose(user, within(dialog).getByLabelText('Logon type'), 'Interactive')
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/a one-time code, is asked to you then/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(body.authMethod).toBe('interactive'))
    expect(body).toMatchObject({ protocol: 'sftp', username: 'deploy' })
    expect(body).not.toHaveProperty('password')
  })

  it('says a stored key is kept unless another file is chosen', async () => {
    mockWorkspaceData({ sites: [{ ...prod, authMethod: 'key', secrets: ['private_key', 'passphrase'] }], collections: [] })
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog', { name: 'Edit site' })

    expect(within(dialog).getByLabelText('Logon type')).toHaveTextContent('Key file')
    expect(within(dialog).getByRole('button', { name: 'Choose a key file' })).toBeInTheDocument()
    expect(within(dialog).getByText('A key is saved. Choose or drop another file to replace it.')).toBeInTheDocument()
  })

  it('swaps the password for the key fields', async () => {
    let body: Record<string, unknown> = {}
    mockKeyInspection()
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
    await choose(user, within(dialog).getByLabelText('Logon type'), 'Key file')
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Name'), 'Keyed')
    await user.type(within(dialog).getByLabelText('Host'), 'k.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'u')
    await user.upload(within(dialog).getByLabelText('Key file'), new File(['KEY'], 'id_ed25519'))
    await user.type(await within(dialog).findByLabelText('Key passphrase'), 'phrase')
    await within(dialog).findByText('Ready to use.')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    await waitFor(() => expect(body.privateKey).toBe('KEY'))
    expect(body).toMatchObject({ authMethod: 'key', passphrase: 'phrase' })
    expect(body.password).toBeUndefined()
  })

  it('configures a proxy and a jump host, in both authentication methods', async () => {
    let body: Record<string, unknown> = {}
    mockKeyInspection()
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
    await choose(user, within(dialog).getByLabelText('Proxy'), 'SOCKS5')
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
    await choose(user, within(dialog).getByLabelText('Jump host logon type'), 'Key file')
    expect(within(dialog).queryByLabelText('Jump host password')).not.toBeInTheDocument()
    await user.upload(within(dialog).getByLabelText('Key file'), new File(['KEY'], 'bastion.ppk'))
    await user.type(await within(dialog).findByLabelText('Jump host key passphrase'), 'phrase')
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
    await choose(user, within(dialog).getByLabelText('Protocol'), 'FTP - File Transfer Protocol')
    expect(within(dialog).queryByText('SSH jump host')).not.toBeInTheDocument()
  }, 40_000)

  it('starts a site inside a collection', async () => {
    const user = await renderSidebar()

    await pick(user, 'Work', 'New site here')

    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    expect(within(dialog).getByLabelText('Collection')).toHaveTextContent('Work')
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

  it('explains a private key the server refuses and opens the tab of that key', async () => {
    let answer = problem(422, 'wrong_passphrase', { key: 'jump' })
    server.use(http.post('/api/v1/workspaces/:id/sites', () => answer))
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'Keyed')
    await user.type(within(dialog).getByLabelText('Host'), 'h')
    await user.type(within(dialog).getByLabelText('Username'), 'u')

    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The passphrase does not open the jump host key.')
    expect(within(dialog).getByRole('tab', { name: 'Proxy and jump host' })).toHaveAttribute('aria-selected', 'true')

    answer = problem(422, 'private_key_is_public', { key: 'site' })
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    expect(await within(dialog).findByText('The private key is a public key. Choose the private key file.')).toBeInTheDocument()
    expect(within(dialog).getByRole('tab', { name: 'General' })).toHaveAttribute('aria-selected', 'true')
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

  it('asks for the password of a site that asks for it when testing, and tests again once it is given', async () => {
    mockWorkspaceData({ sites: [{ ...prod, authMethod: 'ask', secrets: [] }], collections: [] })
    let given = false
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => (given ? HttpResponse.json([]) : problem(409, 'credentials_required'))),
      http.put('/api/v1/workspaces/:id/sites/s1/credential', () => {
        given = true
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = await renderSidebar()
    await pick(user, 'Prod', 'Edit')
    const dialog = await screen.findByRole('dialog', { name: 'Edit site' })
    expect(within(dialog).getByLabelText('Logon type')).toHaveTextContent('Ask for password')
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))
    const ask = await screen.findByRole('dialog', { name: 'Enter the password' })
    await user.click(within(ask).getByRole('button', { name: 'Cancel' }))
    expect(await within(dialog).findByText('This site asks for its password when connecting.')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Test connection' }))
    const again = await screen.findByRole('dialog', { name: 'Enter the password' })
    await user.type(within(again).getByLabelText('Password', { exact: true }), 'typed')
    await user.click(within(again).getByRole('button', { name: 'Connect' }))
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
