import { screen } from '@testing-library/react'
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router'
import { describe, expect, it } from 'vitest'

import { UserMenu } from '@/features/topbar/UserMenu'
import { adminSession, mockSession } from '@/test/handlers'
import { renderWithProviders } from '@/test/render'

import { useSession } from './use-session'

/** Shows who is signed in, or that nobody is. */
function Who() {
  const session = useSession()
  return <p>{session === null ? 'nobody' : session.user.email}</p>
}

/**
 * Covers {@link useSession} and the account avatar while there is no session.
 *
 * Scenarios:
 * - the hook reports nobody while the session loads and the account once it is known
 * - it keeps reporting nobody when the server says there is no session
 * - the avatar has no initials without a session
 */
describe('useSession', () => {
  it('reports nobody while loading and the account afterwards', async () => {
    mockSession(adminSession)

    await renderWithProviders(<Who />)

    expect(screen.getByText('nobody')).toBeInTheDocument()
    expect(await screen.findByText('admin@example.com')).toBeInTheDocument()
  })

  it('reports nobody without a session', async () => {
    mockSession(null)

    await renderWithProviders(<Who />)

    expect(await screen.findByText('nobody')).toBeInTheDocument()
  })

  it('shows an empty avatar without a session', async () => {
    mockSession(null)

    const router = createRouter({ routeTree: createRootRoute({ component: UserMenu }), history: createMemoryHistory() })
    await renderWithProviders(<RouterProvider router={router} />)

    expect(await screen.findByRole('button', { name: /Account$/ })).toHaveTextContent('')
  })
})
