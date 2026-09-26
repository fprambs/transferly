import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { adminSession, mockSession, mockUpdateMe, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'
import { useUiStore } from '@/state/ui-store'

/**
 * Covers the settings page, {@link useThemeChoice} and {@link useAccountTheme}.
 *
 * Scenarios:
 * - the page lists every theme as a choice, with the current one checked
 * - choosing a theme applies it at once and saves it to the account; a failure to save is shown, applied theme stays, and clears next time
 * - following the system turns the theme choices off
 * - the account's theme wins when the session loads, unless it is not a theme this version knows
 * - the language changes from the page
 * - the user menu opens the settings
 */
describe('settings', () => {
  beforeEach(() => {
    useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false })
    mockSession(adminSession)
    mockWorkspaceData()
  })

  it('lists the themes with the current one checked', async () => {
    mockUpdateMe()
    await renderApp({ path: '/settings' })

    const group = await screen.findByRole('radiogroup', { name: 'Themes' })

    expect(within(group).getAllByRole('radio')).toHaveLength(12)
    expect(within(group).getByRole('radio', { name: /Sentinel Dark/ })).toBeChecked()
    expect(within(group).getByRole('radio', { name: /Monokai/ })).not.toBeChecked()
  })

  it('applies a chosen theme at once and saves it to the account', async () => {
    const saved = mockUpdateMe()
    const user = userEvent.setup()
    await renderApp({ path: '/settings' })

    await user.click(await screen.findByRole('radio', { name: /Monokai/ }))

    expect(document.documentElement.dataset.theme).toBe('monokai')
    expect(screen.getByRole('radio', { name: /Monokai/ })).toBeChecked()
    await waitFor(() => expect(saved).toEqual([{ theme: 'monokai' }]))
  })

  it('keeps the applied theme and says so when saving fails, then clears the message', async () => {
    let attempts = 0
    server.use(
      http.patch('/api/v1/me', () => (++attempts === 1 ? problem(500, 'internal') : HttpResponse.json({ ...adminSession.user, theme: 'nord' }))),
    )
    const user = userEvent.setup()
    await renderApp({ path: '/settings' })

    await user.click(await screen.findByRole('radio', { name: /Dracula/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('could not be saved to your account')
    expect(document.documentElement.dataset.theme).toBe('dracula')

    await user.click(screen.getByRole('radio', { name: /Nord/ }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })

  it('turns the theme choices off while following the system', async () => {
    mockUpdateMe()
    const user = userEvent.setup()
    await renderApp({ path: '/settings' })

    await user.click(await screen.findByRole('checkbox', { name: 'Follow the system' }))

    expect(useUiStore.getState().followSystem).toBe(true)
    expect(screen.getByRole('radio', { name: /Monokai/ })).toBeDisabled()
  })

  it('wears the theme of the account when the session loads', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, theme: 'nord' } })

    await renderApp({ path: '/settings' })

    await waitFor(() => expect(useUiStore.getState().themeId).toBe('nord'))
  })

  it('ignores an account theme this version does not know', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, theme: 'from-the-future' } })
    useUiStore.setState({ themeId: 'dracula' })

    await renderApp({ path: '/settings' })
    await screen.findByRole('radiogroup', { name: 'Themes' })

    expect(useUiStore.getState().themeId).toBe('dracula')
  })

  it('changes the language', async () => {
    mockUpdateMe()
    const user = userEvent.setup()
    await renderApp({ path: '/settings' })

    await user.selectOptions(await screen.findByLabelText('Interface language'), 'es')

    expect(await screen.findByRole('heading', { name: 'Ajustes' })).toBeInTheDocument()
  })

  it('opens from the user menu', async () => {
    mockUpdateMe()
    const user = userEvent.setup()
    const { history } = await renderApp()

    await user.click(await screen.findByRole('button', { name: 'Account' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Settings' }))

    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument()
    expect(history.location.pathname).toBe('/settings')
  })
})
