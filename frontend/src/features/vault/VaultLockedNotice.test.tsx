import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockUnlocking, mockVault, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'
import { renderWithProviders } from '@/test/render'
import { Explorer } from '../explorer/Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** The saved site whose password is sealed in the vault. */
const site: Site = {
  id: 's1',
  name: 'Prod',
  protocol: 'sftp',
  host: 'prod.example.com',
  port: 22,
  username: 'deploy',
  authMethod: 'password',
  listTimezone: 'UTC',
  charset: 'utf-8',
  speedLimitBps: 0,
  secrets: ['password'],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** The one file of the site. */
const readme: RemoteEntry = { name: 'readme.txt', path: '/readme.txt', type: 'file', size: 10, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }

/**
 * Covers {@link VaultLockedNotice} where the interface uses it.
 *
 * Scenarios:
 * - typing a password into a new site while the vault is locked explains it at once and unlocks in place, and the explanation goes away
 * - a site saved without any secret does not mention the vault
 * - saving with a locked vault, or a server that says it is locked, asks for the master password at once, naming the site, and saves once it is
 *   unlocked; dismissing it leaves the panel, and saving again asks again
 * - a vault that does not exist yet offers to set it up, from the panel and when saving
 * - opening a saved site whose password is sealed asks for the master password at once, naming the site, and lists the folder once unlocked;
 *   dismissing it leaves the explanation with its button
 */
describe('vault locked notice', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  it('explains a locked vault as soon as a password is typed and unlocks in place', async () => {
    const vault = mockUnlocking()
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    expect(within(dialog).queryByRole('status')).not.toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Password'), 'secret')
    const notice = await within(dialog).findByRole('status')
    expect(notice).toHaveTextContent('Your vault is locked')
    expect(notice).toHaveTextContent('Editing other settings and quick connections do not need it.')

    await user.click(within(notice).getByRole('button', { name: 'Unlock the vault' }))
    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    await user.type(within(unlock).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(unlock).getByRole('button', { name: 'Unlock' }))

    await waitFor(() => expect(vault.unlocked()).toBe(true))
    await waitFor(() => expect(within(dialog).queryByRole('status')).not.toBeInTheDocument())
  })

  it('does not mention the vault when nothing is sealed', async () => {
    mockUnlocking()
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    await user.type(within(dialog).getByLabelText('Name'), 'Plain')

    expect(within(dialog).queryByRole('status')).not.toBeInTheDocument()
  })

  it('asks for the master password when the server says the vault is locked, and saves once it is unlocked', async () => {
    mockVault({ initialized: true, unlocked: true })
    let posts = 0
    server.use(
      http.post('/api/v1/workspaces/:id/sites', () => {
        posts++
        return posts === 1 ? problem(409, 'vault_locked') : HttpResponse.json({ id: 's9' }, { status: 201 })
      }),
    )
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    await user.type(within(dialog).getByLabelText('Name'), 'Prod')
    await user.type(within(dialog).getByLabelText('Host'), 'prod.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'deploy')
    await user.type(within(dialog).getByLabelText('Password'), 'secret')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    mockUnlocking()
    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    expect(unlock).toHaveTextContent('Prod')
    expect(unlock).toHaveTextContent('prod.example.com')
    expect(unlock).toHaveTextContent('deploy')
    expect(within(unlock).getByLabelText('Master password')).toHaveFocus()
    await user.type(within(unlock).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(unlock).getByRole('button', { name: 'Unlock' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New site' })).not.toBeInTheDocument())
    expect(posts).toBe(2)
  })

  it('asks for the master password when saving with a locked vault, and keeps the panel when that is dismissed', async () => {
    const vault = mockUnlocking()
    let posts = 0
    server.use(
      http.post('/api/v1/workspaces/:id/sites', () => {
        posts++
        return HttpResponse.json({ id: 's9' }, { status: 201 })
      }),
    )
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    await user.type(within(dialog).getByLabelText('Name'), 'Prod')
    await user.type(within(dialog).getByLabelText('Host'), 'prod.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'deploy')
    await user.type(within(dialog).getByLabelText('Password'), 'secret')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    expect(posts).toBe(0)
    await user.click(within(unlock).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Unlock the vault' })).not.toBeInTheDocument())
    expect(within(dialog).getByRole('status')).toHaveTextContent('Your vault is locked')

    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))
    const again = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    await user.type(within(again).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(again).getByRole('button', { name: 'Unlock' }))
    await waitFor(() => expect(posts).toBe(1))
    expect(vault.unlocked()).toBe(true)
  })

  it('offers to set the vault up when there is none', async () => {
    mockUnlocking(false)
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    await user.type(within(dialog).getByLabelText('Password'), 'secret')

    const notice = await within(dialog).findByRole('status')
    expect(notice).toHaveTextContent('Set up your vault')
    await user.click(within(notice).getByRole('button', { name: 'Set up the vault' }))
    expect(await screen.findByRole('dialog', { name: 'Set up the vault' })).toBeInTheDocument()
  })

  it('offers to set the vault up when saving a site with a secret and there is none', async () => {
    mockUnlocking(false)
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'New site' }))
    const dialog = await screen.findByRole('dialog', { name: 'New site' })
    await user.type(within(dialog).getByLabelText('Name'), 'Prod')
    await user.type(within(dialog).getByLabelText('Host'), 'prod.example.com')
    await user.type(within(dialog).getByLabelText('Username'), 'deploy')
    await user.type(within(dialog).getByLabelText('Password'), 'secret')
    await user.click(within(dialog).getByRole('button', { name: 'Create site' }))

    expect(await screen.findByRole('dialog', { name: 'Set up the vault' })).toBeInTheDocument()
  })

  it('asks for the master password as soon as a saved site needs it and lists the folder once unlocked', async () => {
    mockWorkspaceData({ sites: [site] })
    const vault = mockUnlocking()
    let listings = 0
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => {
        listings++
        return vault.unlocked() ? HttpResponse.json([readme]) : problem(409, 'vault_locked')
      }),
    )
    const user = userEvent.setup()
    await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/" onNavigate={() => undefined} />)

    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    expect(unlock).toHaveTextContent('Prod')
    expect(unlock).toHaveTextContent('prod.example.com')
    expect(unlock).toHaveTextContent('deploy')
    await user.type(within(unlock).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(unlock).getByRole('button', { name: 'Unlock' }))

    expect(await screen.findByRole('row', { name: /readme.txt/ })).toBeInTheDocument()
    expect(listings).toBeGreaterThanOrEqual(2)
  })

  it('keeps the explanation and its button when the master password is not given', async () => {
    mockUnlocking()
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files', () => problem(409, 'vault_locked')))
    const user = userEvent.setup()
    await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/" onNavigate={() => undefined} />)

    await user.click(within(await screen.findByRole('dialog', { name: 'Unlock the vault' })).getByRole('button', { name: 'Cancel' }))
    const notice = (await screen.findByText('Your vault is locked')).closest('[role="status"]') as HTMLElement
    expect(notice).toHaveTextContent('The password of this site is sealed in your vault.')
    await user.click(within(notice).getByRole('button', { name: 'Unlock the vault' }))
    expect(await screen.findByRole('dialog', { name: 'Unlock the vault' })).toBeInTheDocument()
  })
})
