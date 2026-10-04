import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'
import { useDialogsStore } from '@/state/dialogs-store'
import { useUiStore } from '@/state/ui-store'
import { adminSession, mockSession, mockWorkspaceData } from '@/test/handlers'
import { server } from '@/test/msw'
import { narrowQuery } from '@/lib/layout/use-narrow'
import { renderApp } from '@/test/render-app'

/** A saved site. */
const prod: Site = {
  connector: 'sftp', config: { host: 'prod.example.com', port: '22', username: 'deploy', auth: 'password', list_timezone: 'UTC', charset: 'utf-8' },
  id: 's1',
  name: 'Prod',
  speedLimitBps: 0,
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/**
 * Covers hiding the sites panel (4.10): the rail it leaves, the sheet that shows the sites over the page, and Ctrl+B.
 *
 * Scenarios:
 * - the sidebar hides itself from its header, leaving a rail that shows it again
 * - Ctrl+B (or Cmd+B) shows and hides the panel, except while typing in a field
 * - the rail opens the sites in a sheet, which closes once a site is opened, and starts a quick connection
 * - in a narrow window the sites stay in the rail even when the panel is meant to show, and the rail has no button to show it
 */
describe('sites rail', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [prod] })
    server.use(http.get('/api/v1/workspaces/:id/sites/:siteId/files', () => HttpResponse.json([])))
    useUiStore.setState({ sitesOpen: true })
    useDialogsStore.setState({ quickConnectOpen: false })
  })

  it('hides from its header and comes back from the rail', async () => {
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    await user.click(await screen.findByRole('button', { name: 'Hide the sites panel' }))

    const rail = await screen.findByRole('navigation', { name: 'Sites, panel hidden' })
    expect(useUiStore.getState().sitesOpen).toBe(false)
    expect(screen.queryByRole('tree', { name: 'Sites' })).not.toBeInTheDocument()
    await user.click(within(rail).getByRole('button', { name: 'Show the sites panel' }))
    expect(await screen.findByRole('tree', { name: 'Sites' })).toBeInTheDocument()
  })

  it('shows and hides with Ctrl+B, but not while typing', async () => {
    await renderApp({ path: '/' })
    await screen.findByRole('tree', { name: 'Sites' })

    fireEvent.keyDown(window, { key: 'b', ctrlKey: true })
    await screen.findByRole('navigation', { name: 'Sites, panel hidden' })
    fireEvent.keyDown(window, { key: 'B', metaKey: true })
    await screen.findByRole('tree', { name: 'Sites' })

    const field = document.createElement('input')
    document.body.append(field)
    fireEvent.keyDown(field, { key: 'b', ctrlKey: true })
    fireEvent.keyDown(window, { key: 'b', ctrlKey: true, shiftKey: true })
    fireEvent.keyDown(window, { key: 'b' })
    expect(useUiStore.getState().sitesOpen).toBe(true)
    field.remove()
  })

  it('opens the sites in a sheet and starts a quick connection from the rail', async () => {
    useUiStore.setState({ sitesOpen: false })
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    const rail = await screen.findByRole('navigation', { name: 'Sites, panel hidden' })

    await user.click(within(rail).getByRole('button', { name: 'Browse the sites' }))
    const sheet = await screen.findByRole('dialog', { name: 'Sites' })
    expect(within(sheet).queryByRole('button', { name: 'Hide the sites panel' })).not.toBeInTheDocument()
    await user.dblClick(await within(sheet).findByRole('treeitem', { name: /Prod/ }))
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s1'))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Sites' })).not.toBeInTheDocument())

    await user.click(within(rail).getByRole('button', { name: 'Quick connect' }))
    expect(useDialogsStore.getState().quickConnectOpen).toBe(true)
  })

  it('keeps the sites in the rail in a narrow window', async () => {
    const original = window.matchMedia
    window.matchMedia = ((query: string) => ({ matches: query === narrowQuery, media: query, addEventListener: () => undefined, removeEventListener: () => undefined })) as unknown as typeof window.matchMedia
    useUiStore.setState({ sitesOpen: true, detailsOpen: true })
    await renderApp({ path: '/sites/s1?path=%2F' })

    const rail = await screen.findByRole('navigation', { name: 'Sites, panel hidden' })
    expect(within(rail).queryByRole('button', { name: 'Show the sites panel' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tree', { name: 'Sites' })).not.toBeInTheDocument()
    expect(useUiStore.getState().sitesOpen).toBe(true)
    window.matchMedia = original
  })
})
