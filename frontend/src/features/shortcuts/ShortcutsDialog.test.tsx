import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { useDialogsStore } from '@/state/dialogs-store'
import { adminSession, mockSession, mockWorkspaceData } from '@/test/handlers'
import { renderApp } from '@/test/render-app'

/**
 * Covers {@link ShortcutsDialog}.
 *
 * Scenarios:
 * - ? opens the help outside a field, which lists the shortcuts of the shell, the tabs and the files written for the system, and Escape
 *   closes it
 * - ? typed in a field stays in the field, and with a modifier it does nothing
 * - the command palette opens it too
 */
describe('ShortcutsDialog', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
    useDialogsStore.setState({ shortcutsOpen: false, paletteOpen: false })
  })

  it('opens with ? and lists the shortcuts', async () => {
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    await screen.findByRole('heading', { name: 'Pick a site to start' })

    fireEvent.keyDown(window, { key: '?' })
    const dialog = await screen.findByRole('dialog', { name: 'Keyboard shortcuts' })
    for (const group of ['General', 'Tabs', 'Files']) {
      expect(within(dialog).getByRole('region', { name: group })).toBeInTheDocument()
    }
    expect(within(dialog).getByText('Search or run a command').closest('div')).toHaveTextContent('Ctrl+K')
    expect(within(dialog).getByText('Next tab').closest('div')).toHaveTextContent(/Ctrl\+Tab\s*or\s*Alt\+PageDown/)

    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('leaves ? to a field and ignores it with a modifier', async () => {
    await renderApp({ path: '/' })
    await screen.findByRole('heading', { name: 'Pick a site to start' })

    const field = document.body.appendChild(document.createElement('input'))
    fireEvent.keyDown(field, { key: '?' })
    field.remove()
    fireEvent.keyDown(window, { key: '?', ctrlKey: true })
    expect(useDialogsStore.getState().shortcutsOpen).toBe(false)
  })

  it('opens from the command palette', async () => {
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    await screen.findByRole('heading', { name: 'Pick a site to start' })

    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    const palette = await screen.findByRole('dialog', { name: 'Command palette' })
    await user.click(await within(palette).findByRole('option', { name: /Keyboard shortcuts/ }))

    expect(await screen.findByRole('dialog', { name: 'Keyboard shortcuts' })).toBeInTheDocument()
  })
})
