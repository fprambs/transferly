import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { useUiStore } from '@/state/ui-store'
import { problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'
import { mockTeam, teamId } from '@/test/teams'

/** Opens the members page and the invite dialog. */
async function openInvite() {
  const user = userEvent.setup()
  await renderApp({ path: '/members' })
  await user.click(await screen.findByRole('button', { name: 'Invite member' }))
  return { user, dialog: await screen.findByRole('dialog', { name: 'Invite to Acme Web' }) }
}

/**
 * Covers {@link InviteDialog}.
 *
 * Scenarios:
 * - the owner may invite admins, editors and viewers with editor chosen, and an admin only editors and viewers
 * - an empty address is refused before asking the server, and the server's refusals are explained
 * - a created invitation shows its one-time link with the token in the fragment, who it is for, its role and expiry, and the link can be copied
 * - closing the dialog after creating leaves the invitation in the invitations tab
 */
describe('invite dialog', () => {
  beforeEach(() => {
    useUiStore.setState({ workspaceId: teamId })
  })

  it('offers the roles the inviter may give', async () => {
    mockTeam({ role: 'owner' })
    const owner = await openInvite()

    expect(within(owner.dialog).getAllByRole('radio').map((radio) => (radio as HTMLInputElement).value)).toEqual(['admin', 'editor', 'viewer'])
    expect(within(owner.dialog).getByRole('radio', { name: /Editor/ })).toBeChecked()
  })

  it('offers an admin only editors and viewers', async () => {
    mockTeam({ role: 'admin' })
    const { dialog } = await openInvite()

    expect(within(dialog).getAllByRole('radio').map((radio) => (radio as HTMLInputElement).value)).toEqual(['editor', 'viewer'])
  })

  it('refuses an empty address and explains what the server refuses', async () => {
    mockTeam()
    server.use(http.post(`/api/v1/workspaces/${teamId}/invitations`, () => problem(409, 'already_member')))
    const { user, dialog } = await openInvite()

    await user.click(within(dialog).getByRole('button', { name: 'Create invitation' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Enter an email address.')

    await user.type(within(dialog).getByLabelText('Email'), 'maria@acme.dev')
    await user.click(within(dialog).getByRole('button', { name: 'Create invitation' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('This person is already a member.')

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows the one-time link, copies it and keeps the invitation in the list', async () => {
    const team = mockTeam()
    const { user, dialog } = await openInvite()

    await user.type(within(dialog).getByLabelText('Email'), ' ana@acme.dev ')
    await user.click(within(dialog).getByRole('radio', { name: /Viewer/ }))
    await user.click(within(dialog).getByRole('button', { name: 'Create invitation' }))

    const link = await within(dialog).findByLabelText('One-time link')
    expect(link).toHaveValue(`${window.location.origin}/invite#tok-new-0123456789`)
    expect(team.calls).toEqual([{ method: 'POST', path: `/api/v1/workspaces/${teamId}/invitations`, body: { email: 'ana@acme.dev', role: 'viewer' } }])
    expect(within(dialog).getByText('ana@acme.dev', { selector: 'dd' })).toBeInTheDocument()
    expect(within(dialog).getByText('Viewer', { selector: 'dd' })).toBeInTheDocument()
    expect(within(dialog).getByText(/Oct 3, 2026/)).toBeInTheDocument()

    await user.click(link)
    await user.click(within(dialog).getByRole('button', { name: 'Copy' }))
    expect(await within(dialog).findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(await navigator.clipboard.readText()).toBe(`${window.location.origin}/invite#tok-new-0123456789`)

    await user.click(within(dialog).getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await user.click(screen.getByRole('tab', { name: /Invitations/ }))
    expect(await screen.findByText('ana@acme.dev')).toBeInTheDocument()
  })
})
