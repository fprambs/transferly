import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

import { useTabs } from './tabs-store'

/** Builds a saved site. */
function site(id: string, name: string): Site {
  return {
    connector: 'sftp', config: { host: `${id}.example.com`, port: '22', username: 'deploy', auth: 'password', list_timezone: 'UTC', charset: 'utf-8' },
    id,
    name,
    speedLimitBps: 0,
    secrets: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

/** Answers listings with one file named after the site, and records the sites disconnected. */
function mockServer(): string[] {
  const disconnected: string[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/:siteId/files', ({ params }) => {
      const entry: RemoteEntry = { name: `${params.siteId as string}.txt`, path: `/${params.siteId as string}.txt`, type: 'file', size: 1, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }
      return HttpResponse.json([entry])
    }),
    http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', ({ params }) => {
      disconnected.push(params.siteId as string)
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return disconnected
}

/** The tab bar. */
function bar() {
  return within(screen.getByRole('navigation', { name: 'Connection tabs' }))
}

/** The names of the tabs, the active one marked with an asterisk. */
function tabs(): string[] {
  return bar()
    .getAllByRole('button', { name: /^(?!Close)/ })
        .map((button) => `${(button.textContent ?? '').trim()}${button.getAttribute('aria-current') === 'page' ? '*' : ''}`)
}

/** Waits until the file of a site is in view, in either pane. */
async function showing(siteId: string): Promise<void> {
  await waitFor(() => expect(screen.getAllByRole('grid').some((grid) => within(grid).queryByText(`${siteId}.txt`) !== null)).toBe(true))
}

/**
 * Covers the connection tabs (4.12): {@link TabBar} and opening sites from the sidebar.
 *
 * Scenarios:
 * - the tab of a site with a color label wears it as its top edge
 * - a click shows a site in the active tab and closes the connections of the site it replaces; Ctrl+click, a middle click or the menu open it
 *   in a new tab and keep the others connected; a site that has a tab activates it
 * - a click on a tab shows its site at its folder, and its selection comes back
 * - closing the active tab shows its neighbor, closing the last leaves the sites, a middle click closes, and closing disconnects its site
 * - closing a tab keeps its site connected while another tab shows it in either pane, and disconnects the site of its second pane that no
 *   other tab shows; the local folder has nothing to disconnect
 * - Ctrl+Tab, Ctrl+Shift+Tab, Alt+Page Down and Alt+Page Up move between tabs, Ctrl+W and Alt+W close the active one, except while typing
 * - a tab shows the green dot of a connected site, and says a site that no longer exists is not found, rather than its identifier
 * - the site a click replaces stays connected while the second pane of another tab shows it, and a right click opens nothing
 * - a click also disconnects the site of the second pane it replaces, unless that is the site it opens
 */
describe('connection tabs', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [site('s1', 'Prod'), site('s2', 'Backup'), site('s3', 'Stage')], connected: ['s1'] })
  })

  it('opens sites in the active tab or in new ones', async () => {
    const disconnected = mockServer()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    await user.dblClick(await screen.findByRole('treeitem', { name: /Prod/ }))
    await showing('s1')
    expect(tabs()).toEqual(['Connected Prod*'])

    await user.dblClick(screen.getByRole('treeitem', { name: /Backup/ }))
    await showing('s2')
    expect(tabs()).toEqual(['Backup*'])
    await waitFor(() => expect(disconnected).toEqual(['s1']))

    await user.keyboard('{Control>}')
    await user.dblClick(screen.getByRole('treeitem', { name: /Stage/ }))
    await user.keyboard('{/Control}')
    await showing('s3')
    expect(tabs()).toEqual(['Backup', 'Stage*'])

    await user.pointer({ keys: '[MouseMiddle]', target: screen.getByRole('treeitem', { name: /Prod/ }) })
    await showing('s1')
    expect(tabs()).toEqual(['Backup', 'Stage', 'Connected Prod*'])

    await user.dblClick(screen.getByRole('treeitem', { name: /Backup/ }))
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s2'))
    expect(tabs()).toEqual(['Backup*', 'Stage', 'Connected Prod'])

    await user.dblClick(screen.getByRole('treeitem', { name: /Stage/ }))
    await showing('s3')
    await user.click(within(screen.getByRole('treeitem', { name: /Prod/ })).getByRole('button', { name: 'Actions for Prod' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Open in a new tab' }))
    await showing('s1')
    expect(disconnected).toEqual(['s1'])
  })

  it('switches, remembers and closes tabs', async () => {
    const disconnected = mockServer()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    await user.dblClick(await screen.findByRole('treeitem', { name: /Prod/ }))
    await showing('s1')
    await user.click(within(screen.getByRole('grid')).getByText('s1.txt'))
    await user.keyboard('{Control>}')
    await user.dblClick(screen.getByRole('treeitem', { name: /Backup/ }))
    await user.dblClick(screen.getByRole('treeitem', { name: /Stage/ }))
    await user.keyboard('{/Control}')
    await showing('s3')
    expect(tabs()).toEqual(['Connected Prod', 'Backup', 'Stage*'])

    await user.click(bar().getByRole('button', { name: /^Connected\s*Prod$/ }))
    await showing('s1')
    expect(within(screen.getByRole('complementary', { name: 'Details' })).getByText('/s1.txt')).toBeInTheDocument()

    await user.click(bar().getByRole('button', { name: 'Close Prod' }))
    await showing('s2')
    expect(tabs()).toEqual(['Backup*', 'Stage'])
    await user.click(bar().getByRole('button', { name: 'Close Stage' }))
    expect(tabs()).toEqual(['Backup*'])
    await user.pointer({ keys: '[MouseMiddle]', target: bar().getByRole('button', { name: 'Backup' }) })
    await waitFor(() => expect(history.location.pathname).toBe('/'))
    expect(screen.queryByRole('navigation', { name: 'Connection tabs' })).not.toBeInTheDocument()
    await waitFor(() => expect(disconnected).toEqual(['s1', 's3', 's2']))
  })

  it('keeps a site connected while another tab shows it, and disconnects the second pane of a closed tab', async () => {
    const disconnected = mockServer()
    useTabs.setState({
      tabs: [
        { id: 't1', workspaceId: personalWorkspace.id, siteId: 's1', path: '/', split: 's2' },
        { id: 't2', workspaceId: personalWorkspace.id, siteId: 's3', path: '/', split: 's1' },
        { id: 't3', workspaceId: personalWorkspace.id, siteId: 's2', path: '/', split: 'local' },
      ],
      activeId: 't1',
    })
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    await showing('s1')

    await user.click(bar().getByRole('button', { name: 'Close Prod' }))
    await waitFor(() => expect(tabs()).toEqual(['Stage*', 'Backup']))
    expect(disconnected).toEqual([])
    await user.click(bar().getByRole('button', { name: 'Close Stage' }))
    await waitFor(() => expect(disconnected).toEqual(['s3', 's1']))
    await user.click(bar().getByRole('button', { name: 'Close Backup' }))
    await waitFor(() => expect(disconnected).toEqual(['s3', 's1', 's2']))
  })

  it('moves between tabs and closes them from the keyboard', async () => {
    mockServer()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    await user.dblClick(await screen.findByRole('treeitem', { name: /Prod/ }))
    await showing('s1')
    await user.keyboard('{Control>}')
    await user.dblClick(screen.getByRole('treeitem', { name: /Backup/ }))
    await user.dblClick(screen.getByRole('treeitem', { name: /Stage/ }))
    await user.keyboard('{/Control}')
    await showing('s3')

    fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true })
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s1'))
    fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true, shiftKey: true })
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s3'))
    fireEvent.keyDown(window, { key: 'PageUp', altKey: true })
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s2'))
    fireEvent.keyDown(window, { key: 'PageDown', altKey: true })
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s3'))

    const field = document.createElement('input')
    document.body.append(field)
    fireEvent.keyDown(field, { key: 'w', ctrlKey: true })
    fireEvent.keyDown(window, { key: 'x', ctrlKey: true })
    field.remove()
    expect(tabs()).toHaveLength(3)

    fireEvent.keyDown(window, { key: 'w', ctrlKey: true })
    await waitFor(() => expect(tabs()).toHaveLength(2))
    fireEvent.keyDown(window, { key: 'W', altKey: true })
    await waitFor(() => expect(tabs()).toHaveLength(1))

    act(() => useTabs.getState().deactivate())
    fireEvent.keyDown(window, { key: 'w', ctrlKey: true })
    expect(tabs()).toHaveLength(1)
  })

  it('keeps the site it replaces connected while the second pane of another tab shows it', async () => {
    const disconnected = mockServer()
    useTabs.setState({
      tabs: [
        { id: 't1', workspaceId: personalWorkspace.id, siteId: 's1', path: '/' },
        { id: 't3', workspaceId: personalWorkspace.id, siteId: 's3', path: '/', split: 's1' },
      ],
      activeId: 't1',
    })
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F' })
    await showing('s1')
    await user.dblClick(screen.getByRole('treeitem', { name: /Backup/ }))
    await showing('s2')
    await user.pointer({ keys: '[MouseRight]', target: screen.getByRole('treeitem', { name: /Stage/ }) })
    expect(await screen.findByRole('menuitem', { name: 'Open in a new tab' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument())
    expect(tabs()).toEqual(['Backup*', 'Stage'])
    expect(disconnected).toEqual([])
  })

  it('disconnects the second pane a click replaces, unless it shows the site opened', async () => {
    const disconnected = mockServer()
    useTabs.setState({ tabs: [{ id: 't1', workspaceId: personalWorkspace.id, siteId: 's1', path: '/', split: 's2' }], activeId: 't1' })
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    await showing('s1')

    await user.dblClick(screen.getByRole('treeitem', { name: /Backup/ }))
    await waitFor(() => expect(disconnected).toEqual(['s1']))
    await user.dblClick(screen.getByRole('treeitem', { name: /Stage/ }))
    await waitFor(() => expect(disconnected).toEqual(['s1', 's2']))
  })

  it('tints the tab of a site with a color label', async () => {
    mockWorkspaceData({ sites: [{ ...site('s1', 'Prod'), color: 'red' }, site('s2', 'Backup')], connected: ['s1'] })
    mockServer()
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    await user.dblClick(await screen.findByRole('treeitem', { name: /Prod/ }))
    await showing('s1')

    const tab = within(screen.getByRole('navigation', { name: 'Connection tabs' })).getByRole('button', { name: /Connected ?Prod/ }).parentElement
    expect(tab).toHaveAttribute('data-color', 'red')
    expect(tab).toHaveClass('border-t-2', 'border-t-(--label-red)')
  })

  it('says a site that no longer exists is not found instead of showing its identifier', async () => {
    mockServer()
    useTabs.setState({ tabs: [{ id: 't1', workspaceId: personalWorkspace.id, siteId: 'gone', path: '/' }], activeId: null })
    await renderApp({ path: '/' })
    expect(await screen.findByRole('button', { name: 'Site not found' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'gone' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Close Site not found' })).toBeInTheDocument()
  })
})
