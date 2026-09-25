import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { adminSession, mockSession, mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'
import { useUiStore } from '@/state/ui-store'

/**
 * Covers the top bar: {@link WorkspaceSwitcher}, {@link UserMenu} and the composition of {@link TopBar}.
 *
 * Scenarios:
 * - the avatar shows the initials of the account and the menu its email
 * - signing out calls the server with the anti-CSRF token, forgets the session and returns to login
 * - the switcher shows the workspace in use and lists every workspace
 * - choosing another workspace stores the choice
 * - nothing is shown for the workspace until it loads
 */
describe('top bar', () => {
  beforeEach(() => {
    useUiStore.setState({ workspaceId: null })
    mockSession(adminSession)
  })

  it('shows the initials and the email of the account', async () => {
    mockWorkspaceData()
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'Account' }))

    expect(screen.getByRole('button', { name: 'Account' })).toHaveTextContent('AD')
    expect(await screen.findByText('admin@example.com')).toBeInTheDocument()
  })

  it('signs out and returns to login', async () => {
    mockWorkspaceData()
    let csrf: string | null = null
    server.use(
      http.post('/api/v1/auth/logout', ({ request }) => {
        csrf = request.headers.get('X-CSRF-Token')
        return new HttpResponse(null, { status: 204 })
      }),
      http.get('/api/v1/setup/status', () => HttpResponse.json({ required: false })),
    )
    const user = userEvent.setup()
    const { history } = await renderApp()

    await user.click(await screen.findByRole('button', { name: 'Account' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Sign out' }))

    await waitFor(() => expect(history.location.pathname).toBe('/login'))
    expect(csrf).toBe('csrf-token-1')
  })

  it('switches workspace', async () => {
    const team = { ...personalWorkspace, id: 'team-1', name: 'Team', kind: 'team' as const }
    mockWorkspaceData({ workspaces: [personalWorkspace, team] })
    const user = userEvent.setup()
    await renderApp()

    const switcher = await screen.findByRole('button', { name: 'Workspace' })
    expect(switcher).toHaveTextContent('Personal')

    await user.click(switcher)
    await user.click(await screen.findByRole('menuitem', { name: 'Team' }))

    await waitFor(() => expect(switcher).toHaveTextContent('Team'))
    expect(useUiStore.getState().workspaceId).toBe('team-1')
  })

  it('shows no workspace switcher without workspaces', async () => {
    mockWorkspaceData({ workspaces: [] })
    await renderApp()

    await screen.findByRole('button', { name: 'Account' })

    expect(screen.queryByRole('button', { name: 'Workspace' })).not.toBeInTheDocument()
  })
})
