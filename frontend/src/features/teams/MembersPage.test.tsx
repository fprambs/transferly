import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { useUiStore } from '@/state/ui-store'
import { adminSession, mockSession, mockUnlocking, mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose, optionsOf } from '@/test/select'
import { renderApp } from '@/test/render-app'
import { failTeam, meId, member, mockTeam, teamId } from '@/test/teams'

/** Opens the members page of the team and waits for the table. */
async function openMembers() {
  const user = userEvent.setup()
  const rendered = await renderApp({ path: '/members' })
  await screen.findByText('maria@acme.dev')
  return { user, ...rendered }
}

/** Opens the actions menu of a member and picks an item. */
async function pick(user: ReturnType<typeof userEvent.setup>, email: string, item: string) {
  await user.click(screen.getByRole('button', { name: `Actions for ${email}` }))
  await user.click(await screen.findByRole('menuitem', { name: item }))
}

/**
 * Covers {@link MembersPage} with its table, filter, tabs and the dialogs that change the team.
 *
 * Scenarios:
 * - a personal workspace explains that it has no members and offers to create a team, which opens on the members page
 * - the owner sees every member with their role, the state of their key and who they are, and the role table and the note about keys
 * - the filter narrows the table by email or by the name of the role, and says when nobody matches
 * - the owner changes the role of a member with the drop-down, and a refusal is explained
 * - an admin can change editors and viewers but not other admins or the owner, and has no menu on itself except leaving
 * - editors and viewers see the members but cannot invite, change, remove or see the invitations tab, and can leave
 * - removing a member asks first, explains the key rotation, removes and refreshes the list, and a refusal is explained
 * - ownership goes to a member whose key is sealed after typing the name of the workspace, it cannot go to a member still waiting and a refusal is explained
 * - leaving a team asks first, calls the server and goes back to the personal workspace
 * - the invitations tab lists what is waiting with who invited and when it expires, revokes one, says when there are none and while it loads
 * - the team switcher lists the workspaces with roles and opens the members page
 */
describe('members page', () => {
  beforeEach(() => {
    useUiStore.setState({ workspaceId: teamId })
  })

  it('explains that a personal workspace has no members and offers a team', async () => {
    mockSession(adminSession)
    mockWorkspaceData()
    useUiStore.setState({ workspaceId: null })
    let created: unknown
    server.use(
      http.post('/api/v1/workspaces', async ({ request }) => {
        created = await request.json()
        return HttpResponse.json({ id: teamId, name: 'Acme Web', kind: 'team', role: 'owner', createdAt: '2026-02-01T00:00:00Z' }, { status: 201 })
      }),
    )
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/members' })

    expect(await screen.findByRole('heading', { name: 'Personal workspaces have no other members' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'New team workspace' }))
    const dialog = await screen.findByRole('dialog', { name: 'New team workspace' })
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Enter a name.')

    mockTeam()
    await user.type(within(dialog).getByLabelText('Name'), '  Acme Web ')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New team workspace' })).not.toBeInTheDocument())
    expect(created).toEqual({ name: 'Acme Web' })
    expect(useUiStore.getState().workspaceId).toBe(teamId)
    expect(history.location.pathname).toBe('/members')
  })

  it('explains why a team could not be created', async () => {
    mockSession(adminSession)
    mockWorkspaceData()
    useUiStore.setState({ workspaceId: null })
    server.use(http.post('/api/v1/workspaces', () => HttpResponse.json({ type: 'about:blank', title: 'x', status: 409, code: 'vault_locked' }, { status: 409, headers: { 'Content-Type': 'application/problem+json' } })))
    const user = userEvent.setup()
    await renderApp({ path: '/members' })

    await user.click(await screen.findByRole('button', { name: 'New team workspace' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'Acme')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    const notice = await within(dialog).findByRole('status')
    expect(notice).toHaveTextContent('Your vault is locked')
    mockUnlocking()
    await user.click(within(notice).getByRole('button', { name: 'Unlock the vault' }))
    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    await user.type(within(unlock).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(unlock).getByRole('button', { name: 'Unlock' }))
    await waitFor(() => expect(within(dialog).queryByRole('status')).not.toBeInTheDocument())
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows the members, their roles and keys, and what each role can do', async () => {
    mockTeam()
    await openMembers()

    expect(screen.getByRole('heading', { name: 'Members' })).toBeInTheDocument()
    const list = screen.getByRole('list', { name: 'Members' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(5)
    const me = within(list).getAllByRole('listitem')[0]
    expect(within(me).getByText('You')).toBeInTheDocument()
    expect(within(me).getByText('Owner')).toBeInTheDocument()
    expect(within(screen.getByText('lucia@agency.io').closest('li') as HTMLElement).getByText('Waiting for vault')).toBeInTheDocument()
    expect(within(screen.getByText('pablo@acme.dev').closest('li') as HTMLElement).getByLabelText('Role of pablo@acme.dev')).toHaveTextContent('Viewer')
    expect(screen.getByRole('heading', { name: 'What each role can do' })).toBeInTheDocument()
    expect(screen.getByText('Keys and membership')).toBeInTheDocument()
  })

  it('filters by email and by role', async () => {
    mockTeam()
    const { user } = await openMembers()
    const filter = screen.getByRole('textbox', { name: 'Filter by email or role' })

    await user.type(filter, 'jorge')
    expect(screen.queryByText('maria@acme.dev')).not.toBeInTheDocument()
    expect(screen.getByText('jorge@acme.dev')).toBeInTheDocument()

    await user.clear(filter)
    await user.type(filter, 'viewer')
    expect(screen.getByText('pablo@acme.dev')).toBeInTheDocument()
    expect(screen.queryByText('jorge@acme.dev')).not.toBeInTheDocument()

    await user.clear(filter)
    await user.type(filter, 'nobody at all')
    expect(screen.getByText('No one matches.')).toBeInTheDocument()
  })

  it('changes a role and explains a refusal', async () => {
    const team = mockTeam()
    const { user } = await openMembers()

    await choose(user, screen.getByLabelText('Role of jorge@acme.dev'), 'Admin')
    await waitFor(() => expect(team.calls).toContainEqual({ method: 'PATCH', path: `/api/v1/workspaces/${teamId}/members/u-jorge`, body: { role: 'admin' } }))
    await waitFor(() => expect(screen.getByLabelText('Role of jorge@acme.dev')).toHaveTextContent('Admin'))

    failTeam('patch', '/members/:userId', 403, 'forbidden')
    await choose(user, screen.getByLabelText('Role of pablo@acme.dev'), 'Editor')
    expect(await screen.findByRole('alert')).toHaveTextContent('Your role does not allow this.')
  })

  it('lets an admin manage editors and viewers only', async () => {
    mockTeam({ role: 'admin' })
    await openMembers()

    expect(screen.getByLabelText('Role of jorge@acme.dev')).toBeInTheDocument()
    expect(screen.queryByLabelText('Role of maria@acme.dev')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Invite member' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: `Actions for ${adminSession.user.email}` })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Actions for maria@acme.dev' })).not.toBeInTheDocument()
  })

  it('shows editors and viewers the members without any way to change them', async () => {
    mockTeam({ role: 'editor' })
    const { user } = await openMembers()

    expect(screen.queryByRole('button', { name: 'Invite member' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: /Invitations/ })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Role of pablo@acme.dev')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: `Actions for ${adminSession.user.email}` }))
    expect(await screen.findByRole('menuitem', { name: 'Leave team' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: 'Remove from team' })).not.toBeInTheDocument()
  })

  it('removes a member after explaining the key rotation', async () => {
    const team = mockTeam()
    const { user } = await openMembers()

    await pick(user, 'jorge@acme.dev', 'Remove from team')
    const dialog = await screen.findByRole('dialog', { name: 'Remove jorge@acme.dev?' })
    expect(within(dialog).getByText('The workspace key will be rotated')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(team.calls).toHaveLength(0)

    await pick(user, 'jorge@acme.dev', 'Remove from team')
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Remove and rotate key' }))

    await waitFor(() => expect(screen.queryByText('jorge@acme.dev')).not.toBeInTheDocument())
    expect(team.calls).toEqual([{ method: 'DELETE', path: `/api/v1/workspaces/${teamId}/members/u-jorge`, body: undefined }])
  })

  it('explains why a member could not be removed', async () => {
    mockTeam()
    failTeam('delete', '/members/:userId', 409, 'owner_bound')
    const { user } = await openMembers()

    await pick(user, 'pablo@acme.dev', 'Remove from team')
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Remove and rotate key' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The owner cannot be changed')
    expect(screen.getByText('pablo@acme.dev')).toBeInTheDocument()
  })

  it('transfers ownership to a sealed member after typing the name', async () => {
    const team = mockTeam()
    const { user } = await openMembers()

    await user.click(screen.getByRole('button', { name: 'Actions for lucia@agency.io' }))
    expect(await screen.findByRole('menuitem', { name: 'Transfer ownership' })).toHaveAttribute('aria-disabled', 'true')
    await user.keyboard('{Escape}')

    await pick(user, 'maria@acme.dev', 'Transfer ownership')
    const dialog = await screen.findByRole('dialog', { name: 'Transfer ownership' })
    expect((await optionsOf(user, within(dialog).getByLabelText('New owner'))).join(' ')).not.toContain('lucia@agency.io')
    const confirm = within(dialog).getByRole('button', { name: 'Transfer' })
    expect(confirm).toBeDisabled()

    await choose(user, within(dialog).getByLabelText('New owner'), /pablo@acme.dev/)
    await user.type(within(dialog).getByLabelText('Type the workspace name to confirm'), 'Acme Web')
    await user.click(confirm)

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(team.calls).toEqual([{ method: 'POST', path: `/api/v1/workspaces/${teamId}/transfer`, body: { userId: 'u-pablo' } }])
  })

  it('explains a refused transfer', async () => {
    mockTeam()
    failTeam('post', '/transfer', 409, 'owner_bound')
    const { user } = await openMembers()

    await pick(user, 'maria@acme.dev', 'Transfer ownership')
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Type the workspace name to confirm'), 'Acme Web')
    await user.click(within(dialog).getByRole('button', { name: 'Transfer' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The owner cannot be changed')
  })

  it('does not transfer with the wrong name, even pressing Enter', async () => {
    const team = mockTeam()
    const { user } = await openMembers()

    await pick(user, 'maria@acme.dev', 'Transfer ownership')
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Type the workspace name to confirm'), 'Acme Wob{Enter}')

    expect(within(dialog).getByRole('button', { name: 'Transfer' })).toBeDisabled()
    expect(team.calls).toHaveLength(0)
  })

  it('leaves a team and goes back to the personal workspace', async () => {
    const team = mockTeam({ role: 'editor' })
    const { user, history } = await openMembers()

    await pick(user, adminSession.user.email, 'Leave team')
    const dialog = await screen.findByRole('dialog', { name: 'Leave Acme Web?' })
    await user.click(within(dialog).getByRole('button', { name: 'Leave' }))

    await waitFor(() => expect(history.location.pathname).toBe('/'))
    expect(team.calls).toEqual([{ method: 'POST', path: `/api/v1/workspaces/${teamId}/leave`, body: undefined }])
    expect(useUiStore.getState().workspaceId).toBeNull()
  })

  it('explains why leaving failed', async () => {
    mockTeam({ role: 'editor' })
    failTeam('post', '/leave', 409, 'owner_bound')
    const { user } = await openMembers()

    await pick(user, adminSession.user.email, 'Leave team')
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Leave' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The owner cannot be changed')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  })

  it('lists and revokes the invitations', async () => {
    const team = mockTeam({
      invitations: [{ id: 'i-1', email: 'ana@acme.dev', role: 'editor', invitedBy: 'maria@acme.dev', createdAt: '2026-09-20T00:00:00Z', expiresAt: '2026-09-27T00:00:00Z' }, { id: 'i-2', email: 'bob@acme.dev', role: 'viewer', createdAt: '2026-09-20T00:00:00Z', expiresAt: '2026-09-27T00:00:00Z' }],
    })
    const { user } = await openMembers()

    await user.click(screen.getByRole('tab', { name: /Invitations/ }))
    const list = await screen.findByRole('list', { name: 'Invitations' })
    expect(within(list).getByText('Invited by maria@acme.dev')).toBeInTheDocument()
    await user.click(within(list).getByRole('button', { name: 'Revoke the invitation of ana@acme.dev' }))

    await waitFor(() => expect(screen.queryByText('ana@acme.dev')).not.toBeInTheDocument())
    expect(team.calls).toEqual([{ method: 'DELETE', path: `/api/v1/workspaces/${teamId}/invitations/i-1`, body: undefined }])
    await user.click(screen.getByRole('button', { name: 'Revoke the invitation of bob@acme.dev' }))
    expect(await screen.findByText('No invitations are waiting.')).toBeInTheDocument()
  })

  it('shows the loading state while the invitations arrive', async () => {
    mockTeam()
    server.use(
      http.get(`/api/v1/workspaces/${teamId}/invitations`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 1200))
        return HttpResponse.json([])
      }),
    )
    const { user } = await openMembers()

    await user.click(screen.getByRole('tab', { name: /Invitations/ }))

    expect(await screen.findByText('Loading invitations…')).toBeInTheDocument()
    expect(await screen.findByText('No invitations are waiting.')).toBeInTheDocument()
  })

  it('explains why an invitation could not be revoked', async () => {
    mockTeam({ invitations: [{ id: 'i-1', email: 'ana@acme.dev', role: 'editor', createdAt: '2026-09-20T00:00:00Z', expiresAt: '2026-09-27T00:00:00Z' }] })
    failTeam('delete', '/invitations/:id', 403, 'forbidden')
    const { user } = await openMembers()

    await user.click(screen.getByRole('tab', { name: /Invitations/ }))
    await user.click(await screen.findByRole('button', { name: 'Revoke the invitation of ana@acme.dev' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Your role does not allow this.')
  })

  it('shows the loading state while the members arrive', async () => {
    mockTeam()
    server.use(http.get(`/api/v1/workspaces/${teamId}/members`, async () => { await new Promise((resolve) => setTimeout(resolve, 100)); return HttpResponse.json([member(meId, 'admin@example.com', 'owner')]) }))
    await renderApp({ path: '/members' })

    expect(await screen.findByText('Loading members…')).toBeInTheDocument()
    expect(await screen.findByRole('list', { name: 'Members' })).toBeInTheDocument()
  })

  it('lists the workspaces with roles in the switcher and opens the members page', async () => {
    mockTeam({ role: 'admin' })
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })

    await user.click(await screen.findByRole('button', { name: 'Workspace' }))
    expect(await screen.findByRole('menuitem', { name: /Acme Web\s*Admin/ })).toBeInTheDocument()
    await user.click(screen.getByRole('menuitem', { name: 'Members' }))

    await waitFor(() => expect(history.location.pathname).toBe('/members'))
  })

  it('offers a new team from the switcher', async () => {
    mockSession(adminSession)
    mockWorkspaceData({ workspaces: [personalWorkspace] })
    useUiStore.setState({ workspaceId: null })
    const user = userEvent.setup()
    await renderApp({ path: '/' })

    await user.click(await screen.findByRole('button', { name: 'Workspace' }))
    const create = await screen.findByRole('menuitem', { name: 'New team workspace' })
    expect(screen.queryByRole('menuitem', { name: 'Members' })).not.toBeInTheDocument()
    await user.click(create)

    const dialog = await screen.findByRole('dialog', { name: 'New team workspace' })
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
