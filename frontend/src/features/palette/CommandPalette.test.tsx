import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSecurity, mockSession, mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'
import { useDialogsStore } from '@/state/dialogs-store'
import { useUiStore } from '@/state/ui-store'

import { useTabs } from '../tabs/tabs-store'

/** Builds a saved site. */
function site(id: string, name: string): Site {
  return {
    id,
    name,
    protocol: 'sftp',
    host: `${id}.example.com`,
    port: 22,
    username: 'deploy',
    authMethod: 'password',
    listTimezone: 'UTC',
    charset: 'utf-8',
    speedLimitBps: 0,
    secrets: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

/** Answers listings with one file named after the site, counts them, and records the sites disconnected. */
function mockServer(): { listings: number; disconnected: string[] } {
  const seen = { listings: 0, disconnected: [] as string[] }
  server.use(
    http.get('/api/v1/workspaces/:id/sites/:siteId/files', ({ params }) => {
      seen.listings++
      const entry: RemoteEntry = { name: `${params.siteId as string}.txt`, path: `/${params.siteId as string}.txt`, type: 'file', size: 1, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }
      return HttpResponse.json([entry])
    }),
    http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', ({ params }) => {
      seen.disconnected.push(params.siteId as string)
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return seen
}

/** Opens the palette with the shortcut, once the shell is in place, and returns it. */
async function openPalette(meta = false) {
  await screen.findByRole('button', { name: /Search or run a command…/, hidden: true })
  fireEvent.keyDown(window, { key: 'k', ctrlKey: !meta, metaKey: meta })
  return screen.findByRole('dialog', { name: 'Command palette' })
}

/** The option of the palette with a name, once it is drawn. */
function option(palette: HTMLElement, name: string | RegExp): Promise<HTMLElement> {
  return within(palette).findByRole('option', { name })
}

/**
 * Covers the command palette (4.14c).
 *
 * Scenarios:
 * - Ctrl+K or ⌘K opens and closes it, and so does the button of the top bar, whose shortcut is written for the system: Ctrl+K on Windows
 *   and Linux, ⌘K on Apple systems
 * - with no site open it offers quick connect, the sites, the panels and the pages, a search that matches nothing says so, and opening
 *   it again starts with an empty search
 * - on an open site it refreshes, disconnects and focuses the filter of the folder; canceling is off while nothing runs
 * - it lists the tabs, marking the current one, and goes to another
 * - it lists the bookmarks of the site in focus and opens one in its pane, the second pane included
 * - a search by host finds a site, which opens, and connected sites say so
 * - the panels are shown and hidden, with the shortcut of the sites panel
 * - the pages open, members and the audit log only in a team whoever may see them, administration only for administrators
 */
describe('command palette', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [site('s1', 'Prod'), site('s2', 'Backup')], connected: ['s2'], bookmarks: [{ id: 'b1', name: 'Logs', path: '/var/log', createdAt: '2026-01-01T00:00:00Z' }] })
    useUiStore.setState({ sitesOpen: true, logOpen: true, detailsOpen: true, queueOpen: true })
  })

  afterEach(() => {
    Object.defineProperty(navigator, 'platform', { value: '', configurable: true })
  })

  it('opens and closes with its shortcut and its button', async () => {
    mockServer()
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    const button = await screen.findByRole('button', { name: /Search or run a command…/ })
    expect(button).toHaveTextContent('Ctrl+K')

    await openPalette()
    fireEvent.keyDown(window, { key: 'K', ctrlKey: true })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Command palette' })).not.toBeInTheDocument())
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true, shiftKey: true })
    fireEvent.keyDown(window, { key: 'j', ctrlKey: true })
    expect(screen.queryByRole('dialog', { name: 'Command palette' })).not.toBeInTheDocument()
    await openPalette(true)
    await user.keyboard('{Escape}')
    await waitFor(() => expect(useDialogsStore.getState().paletteOpen).toBe(false))
    await user.click(button)
    expect(await screen.findByRole('dialog', { name: 'Command palette' })).toBeInTheDocument()
  })

  it('writes its shortcut for an Apple system', async () => {
    Object.defineProperty(navigator, 'platform', { value: 'MacIntel', configurable: true })
    await renderApp({ path: '/' })

    expect(await screen.findByRole('button', { name: /Search or run a command…/ })).toHaveTextContent('⌘K')
  })

  it('offers quick connect, sites, panels and pages with no site open, and says when nothing matches', async () => {
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    const palette = await openPalette()

    expect(within(palette).queryByRole('option', { name: /Refresh/ })).not.toBeInTheDocument()
    expect(await option(palette, /Prod/)).toBeInTheDocument()
    await user.type(within(palette).getByRole('combobox'), 'zzzz-nothing')
    expect(within(palette).getByText('No command or site matches.')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    const again = await openPalette()
    expect(within(again).getByRole('combobox')).toHaveValue('')
    await user.click(await option(again, /Quick connect/))
    expect(useDialogsStore.getState()).toMatchObject({ paletteOpen: false, quickConnectOpen: true })
  })

  it('acts on the site in view', async () => {
    const seen = mockServer()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F' })
    await within(await screen.findByRole('grid')).findByText('s1.txt')
    const listed = seen.listings

    let palette = await openPalette()
    expect(await option(palette, /Cancel the current operation/)).toHaveAttribute('aria-disabled', 'true')
    await user.click(await option(palette, /Refresh the folder/))
    await waitFor(() => expect(seen.listings).toBe(listed + 1))

    palette = await openPalette()
    await user.click(await option(palette, /Filter this folder/))
    await waitFor(() => expect(screen.getByRole('searchbox', { name: 'Filter this folder' })).toHaveFocus())

    palette = await openPalette()
    await user.click(await option(palette, /Disconnect from the server/))
    await waitFor(() => expect(seen.disconnected).toEqual(['s1']))
    await waitFor(() => expect(history.location.pathname).toBe('/'))
  })

  it('goes to a tab and opens a bookmark in its pane', async () => {
    mockServer()
    useTabs.setState({
      tabs: [
        { id: 't1', workspaceId: personalWorkspace.id, siteId: 's1', path: '/', split: 's2', splitPath: '/' },
        { id: 't2', workspaceId: personalWorkspace.id, siteId: 's2', path: '/deep' },
      ],
      activeId: 't1',
    })
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F&split=s2&splitPath=%2F' })
    await within(await screen.findByRole('group', { name: 'Second pane' })).findByRole('grid', {}, { timeout: 10_000 })

    let palette = await openPalette()
    expect(await option(palette, /Prod.*Current/)).toBeInTheDocument()
    await user.click(await option(palette, /Logs/))
    await waitFor(() => expect(history.location.search).toContain('path=%2Fvar%2Flog'))

    fireEvent.pointerDown(within(screen.getByRole('group', { name: 'Second pane', hidden: true })).getByRole('grid', { hidden: true }))
    palette = await openPalette()
    await user.click(await option(palette, /Logs/))
    await waitFor(() => expect(history.location.search).toContain('splitPath=%2Fvar%2Flog'))

    palette = await openPalette()
    await user.click(within(palette).getAllByRole('option', { name: /Backup/ })[0])
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s2'))
    expect(history.location.search).toContain('path=%2Fdeep')
  })

  it('finds a site by its host and opens it', async () => {
    mockServer()
    mockWorkspaceData({
      sites: [site('s1', 'Prod'), site('s2', 'Backup')],
      connected: ['s2'],
      quick: [{ id: 'q1', name: '198.51.100.7', protocol: 'sftp', host: '198.51.100.7', port: 22, username: 'deploy', authMethod: 'password', listTimezone: 'UTC', charset: 'utf-8', createdAt: '2026-01-01T00:00:00Z' }],
    })
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    const palette = await openPalette()

    expect(await option(palette, /Backup.*Connected/)).toBeInTheDocument()
    expect(await option(palette, /198\.51\.100\.7/)).toBeInTheDocument()
    await user.type(within(palette).getByRole('combobox'), 's1.example')
    await user.click(await option(palette, /Prod/))
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s1'))
  })

  it('shows and hides the panels', async () => {
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    let palette = await openPalette()

    expect(await option(palette, /Hide the sites panel/)).toHaveTextContent('Ctrl+B')
    await user.click(await option(palette, /Hide the transfers panel/))
    expect(useUiStore.getState().queueOpen).toBe(false)
    palette = await openPalette()
    await user.click(await option(palette, /Show the transfers panel/))
    expect(useUiStore.getState().queueOpen).toBe(true)
  })

  it('opens the pages the account may see', async () => {
    mockSecurity()
    server.use(
      http.get('/api/v1/workspaces/:id/known-hosts', () => HttpResponse.json([])),
      http.get('/api/v1/admin/users', () => HttpResponse.json([])),
    )
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    let palette = await openPalette()

    expect(within(palette).queryByRole('option', { name: 'Members' })).not.toBeInTheDocument()
    await user.click(await option(palette, 'Trusted servers'))
    await waitFor(() => expect(history.location.search).toContain('settings=servers'))
    palette = await openPalette()
    await user.click(await option(palette, 'Settings'))
    await waitFor(() => expect(history.location.search).toContain('settings=general'))
    palette = await openPalette()
    await user.click(await option(palette, 'Administration'))
    await waitFor(() => expect(history.location.search).toContain('settings=admin'))
  })

  it('opens the pages of a team whoever may see them', async () => {
    const team = { ...personalWorkspace, id: 'team-1', name: 'Acme', kind: 'team' as const, role: 'owner' as const }
    mockWorkspaceData({ workspaces: [team] })
    server.use(
      http.get('/api/v1/workspaces/:id/members', () => HttpResponse.json([])),
      http.get('/api/v1/workspaces/:id/invitations', () => HttpResponse.json([])),
      http.get('/api/v1/workspaces/:id/audit', () => HttpResponse.json([])),
    )
    mockSession({ ...adminSession, user: { ...adminSession.user, role: 'user' } })
    act(() => useUiStore.getState().setWorkspace('team-1'))
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    let palette = await openPalette()

    expect(within(palette).queryByRole('option', { name: 'Administration' })).not.toBeInTheDocument()
    await user.click(await option(palette, 'Members'))
    await waitFor(() => expect(history.location.search).toContain('settings=members'))
    palette = await openPalette()
    await user.click(await option(palette, 'Audit log'))
    await waitFor(() => expect(history.location.search).toContain('settings=audit'))
  })
})
