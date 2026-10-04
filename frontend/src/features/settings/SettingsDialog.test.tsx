import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { adminSession, mockSecurity, mockSession, mockWorkspaceData } from '@/test/handlers'
import { renderApp } from '@/test/render-app'

/** The side bar of the settings dialog. */
function sections(): HTMLElement {
  return screen.getByRole('navigation', { name: 'Settings' })
}

/**
 * Covers {@link SettingsDialog} and {@link useSettingsDialog}.
 *
 * Scenarios:
 * - the address opens the dialog on its section, marked as the current one, over the start page
 * - the side bar moves to another section and keeps it in the address, and shows the version of the instance
 * - an administrator sees the administration; a personal workspace has no audit trail, and a user who is not an administrator has no
 *   administration
 * - Escape closes the dialog and leaves the page under it as it was
 */
describe('settings dialog', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
    mockSecurity()
  })

  it('opens on the section of the address and moves to another one', async () => {
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/?settings=general' })

    expect(await screen.findByRole('heading', { level: 1, name: 'General' })).toBeInTheDocument()
    expect(within(sections()).getByRole('button', { name: 'General' })).toHaveAttribute('aria-current', 'page')

    await user.click(within(sections()).getByRole('button', { name: 'Security' }))

    expect(await screen.findByRole('heading', { level: 1, name: 'Security' })).toBeInTheDocument()
    expect(within(sections()).getByRole('button', { name: 'Security' })).toHaveAttribute('aria-current', 'page')
    expect(history.location.search).toContain('settings=security')
    expect(within(sections()).getByText('Transferly v0.5.0')).toBeInTheDocument()
  })

  it('lists the sections the account may see', async () => {
    await renderApp({ path: '/?settings=general' })
    await screen.findByRole('heading', { level: 1, name: 'General' })

    const names = within(sections())
      .getAllByRole('button')
      .map((button) => button.textContent)
    expect(names).toEqual(['General', 'Security', 'Members', 'Trusted servers', 'Administration'])
  })

  it('hides the administration from a user who is not an administrator', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, role: 'user' } })
    await renderApp({ path: '/?settings=general' })
    await screen.findByRole('heading', { level: 1, name: 'General' })

    expect(within(sections()).queryByRole('button', { name: 'Administration' })).not.toBeInTheDocument()
  })

  it('closes with Escape and leaves the page under it', async () => {
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/?settings=general' })
    await screen.findByRole('heading', { level: 1, name: 'General' })

    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Settings' })).not.toBeInTheDocument())
    expect(history.location.pathname).toBe('/')
    expect(history.location.search).not.toContain('settings')
  })
})
