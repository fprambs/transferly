import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { useUiStore } from '@/state/ui-store'
import { adminSession, mockSecurity, mockSession, mockWorkspaceData, personalWorkspace, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

import { useConnectionState } from './connection-store'

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

/** A file named after the site and the folder it is in. */
function entry(siteId: string, folder: string): RemoteEntry {
  return { name: `${siteId}${folder.replaceAll('/', '-')}.txt`, path: `${folder}/x`, type: 'file', size: 1, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }
}

/** Answers listings, holding those of folders named "slow" until the request is given up, and counts them by site. */
function mockListings(): Record<string, number> {
  const asked: Record<string, number> = {}
  server.use(
    http.get('/api/v1/workspaces/:id/sites/:siteId/files', async ({ params, request }) => {
      const siteId = params.siteId as string
      const folder = new URL(request.url).searchParams.get('path') ?? '/'
      asked[siteId] = (asked[siteId] ?? 0) + 1
      if (folder.endsWith('slow')) {
        await delay('infinite')
      }
      return HttpResponse.json([entry(siteId, folder)])
    }),
  )
  return asked
}

/** The toolbar of the shell. */
function toolbar() {
  return within(screen.getByRole('toolbar', { name: 'Toolbar' }))
}

/**
 * Covers {@link Toolbar} and the connection states it drives in the explorer (4.9b).
 *
 * Scenarios:
 * - with no site open the connection actions are off, and the switches of the sites, log, details and transfers panels still work; the toolbar is not shown outside the explorer
 * - refresh lists the folder in view again; the toolbar names the connection it acts on
 * - cancel is on only while the connection is busy, stops the listing, and the explorer says it was canceled with a way to try again
 * - disconnect asks the server to close the connections and takes the site out of the address (the main pane leaves for the start page,
 *   the second pane closes), so a reload does not connect again; coming back to it shows it disconnected until the user connects again,
 *   and opening it from the sidebar connects it again too; a failed disconnect says so and changes nothing
 * - the toolbar acts on the pane the user clicked last, the second pane included
 * - opening another site from the sidebar closes the connections of the one it replaces, and reopening the same site does not; a connected
 *   site disconnects from its menu too
 */
describe('Toolbar', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [site('s1', 'Prod'), site('s2', 'Backup')] })
    useConnectionState.setState({ focused: null, disconnected: {} })
    useUiStore.setState({ logOpen: true, sitesOpen: true, detailsOpen: true, queueOpen: true })
  })

  it('is off with no site open, except the panel switches', async () => {
    const user = userEvent.setup()
    await renderApp({ path: '/' })
    await screen.findByRole('toolbar', { name: 'Toolbar' })

    expect(toolbar().getByRole('button', { name: 'Refresh the folder' })).toBeDisabled()
    expect(toolbar().getByRole('button', { name: 'Cancel the current operation' })).toBeDisabled()
    expect(toolbar().getByRole('button', { name: 'Disconnect from the server' })).toBeDisabled()
    for (const [name, key] of [
      ['Sites panel', 'sitesOpen'],
      ['Log panel', 'logOpen'],
      ['Details panel', 'detailsOpen'],
      ['Transfers panel', 'queueOpen'],
    ] as const) {
      expect(toolbar().getByRole('button', { name })).toHaveAttribute('aria-pressed', 'true')
      await user.click(toolbar().getByRole('button', { name }))
      expect(useUiStore.getState()[key]).toBe(false)
      expect(toolbar().getByRole('button', { name })).toHaveAttribute('aria-pressed', 'false')
    }
  })

  it('is shown with the explorer only, not on the settings page', async () => {
    mockSecurity()
    await renderApp({ path: '/settings' })
    await screen.findByRole('heading', { level: 2, name: 'Settings' })
    expect(screen.queryByRole('toolbar', { name: 'Toolbar' })).not.toBeInTheDocument()
  })

  it('refreshes the folder in view and names the connection', async () => {
    const asked = mockListings()
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F' })
    await within(await screen.findByRole('grid')).findByText('s1-.txt')
    expect(await toolbar().findByText('Connection: Prod')).toBeInTheDocument()

    await user.click(toolbar().getByRole('button', { name: 'Refresh the folder' }))
    await waitFor(() => expect(asked.s1).toBe(2))
  })

  it('cancels a listing that takes too long', async () => {
    mockListings()
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2Fslow' })
    await screen.findByRole('toolbar', { name: 'Toolbar' })
    await screen.findByText(/Connecting to/)
    const cancel = toolbar().getByRole('button', { name: 'Cancel the current operation' })
    await waitFor(() => expect(cancel).toBeEnabled())

    await user.click(cancel)
    expect(await screen.findByText('Listing /slow was canceled.')).toBeInTheDocument()
    expect(cancel).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(cancel).toBeEnabled())
  })

  it('disconnects, leaves the site, and connects again when asked', async () => {
    const asked = mockListings()
    let disconnects = 0
    let reply = (): Response => (disconnects++, new HttpResponse(null, { status: 204 }))
    server.use(http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', () => reply()))
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F' })
    await within(await screen.findByRole('grid')).findByText('s1-.txt')

    await user.click(toolbar().getByRole('button', { name: 'Disconnect from the server' }))
    await waitFor(() => expect(history.location.pathname).toBe('/'))
    expect(disconnects).toBe(1)
    expect(screen.queryByRole('grid')).not.toBeInTheDocument()
    expect(toolbar().getByRole('button', { name: 'Refresh the folder' })).toBeDisabled()

    history.back()
    expect(await screen.findByText('Disconnected from Prod')).toBeInTheDocument()
    expect(screen.queryByRole('grid')).not.toBeInTheDocument()
    expect(toolbar().getByText('Disconnected: Prod')).toBeInTheDocument()
    expect(toolbar().getByRole('button', { name: 'Disconnect from the server' })).toBeDisabled()
    expect(asked.s1).toBe(1)
    await user.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await within(await screen.findByRole('grid')).findByText('s1-.txt')).toBeInTheDocument()
    expect(asked.s1).toBe(2)

    await user.click(toolbar().getByRole('button', { name: 'Disconnect from the server' }))
    await waitFor(() => expect(history.location.pathname).toBe('/'))
    await user.dblClick(screen.getByRole('treeitem', { name: /Prod/ }))
    expect(await within(await screen.findByRole('grid')).findByText('s1-.txt')).toBeInTheDocument()

    reply = () => problem(500, 'internal_error')
    await user.click(toolbar().getByRole('button', { name: 'Disconnect from the server' }))
    expect(await toolbar().findByRole('alert')).toHaveTextContent('Could not disconnect. Try again.')
    expect(history.location.pathname).toBe('/sites/s1')
  })

  it('closes the second pane when it is the one disconnected', async () => {
    mockListings()
    server.use(http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', () => new HttpResponse(null, { status: 204 })))
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })
    await user.click(await within(await within(pane).findByRole('grid')).findByText('s2-.txt'))

    await user.click(toolbar().getByRole('button', { name: 'Disconnect from the server' }))
    await waitFor(() => expect(screen.queryByRole('group', { name: 'Second pane' })).not.toBeInTheDocument())
    expect(history.location.pathname).toBe('/sites/s1')
    expect(history.location.search).not.toContain('split')
  })

  it('names a disconnected connection it does not know by its identifier', async () => {
    mockListings()
    useConnectionState.setState({ disconnected: { [`${personalWorkspace.id}/gone`]: true } })
    await renderApp({ path: '/sites/gone?path=%2F' })
    expect(await screen.findByText('Disconnected from gone')).toBeInTheDocument()
  })

  it('closes the connections of the site it leaves when another is opened from the sidebar', async () => {
    mockListings()
    const disconnected: string[] = []
    server.use(
      http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', ({ params }) => {
        disconnected.push(params.siteId as string)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/' })
    await user.dblClick(await screen.findByRole('treeitem', { name: /Prod/ }))
    await within(await screen.findByRole('grid')).findByText('s1-.txt')
    expect(disconnected).toEqual([])

    await user.dblClick(screen.getByRole('treeitem', { name: /Prod/ }))
    expect(disconnected).toEqual([])
    await user.dblClick(screen.getByRole('treeitem', { name: /Backup/ }))
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s2'))
    await waitFor(() => expect(disconnected).toEqual(['s1']))
    expect(useConnectionState.getState().disconnected).toEqual({})

    server.use(http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', () => problem(500, 'internal_error')))
    await user.dblClick(screen.getByRole('treeitem', { name: /Prod/ }))
    await waitFor(() => expect(history.location.pathname).toBe('/sites/s1'))
    expect(await within(await screen.findByRole('grid')).findByText('s1-.txt')).toBeInTheDocument()
  })

  it('disconnects from the menu of a connected site in the sidebar', async () => {
    mockWorkspaceData({ sites: [site('s1', 'Prod'), site('s2', 'Backup')], connected: ['s1', 's2'] })
    mockListings()
    server.use(http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', () => new HttpResponse(null, { status: 204 })))
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F' })
    const backup = await screen.findByRole('treeitem', { name: /Backup/ })
    await within(backup).findByText('Connected')
    await user.click(within(backup).getByRole('button', { name: 'Actions for Backup' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Disconnect' }))
    await waitFor(() => expect(useConnectionState.getState().disconnected).not.toEqual({}))
    expect(history.location.pathname).toBe('/sites/s1')

    const prod = screen.getByRole('treeitem', { name: /Prod/ })
    await user.click(within(prod).getByRole('button', { name: 'Actions for Prod' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Disconnect' }))
    await waitFor(() => expect(history.location.pathname).toBe('/'))
  })

  it('acts on the pane clicked last', async () => {
    const asked = mockListings()
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })
    await within(await within(pane).findByRole('grid')).findByText('s2-.txt')
    expect(toolbar().getByText('Connection: Prod')).toBeInTheDocument()

    await user.click(within(within(pane).getByRole('grid')).getByText('s2-.txt'))
    expect(toolbar().getByText('Connection: Backup')).toBeInTheDocument()
    await user.click(toolbar().getByRole('button', { name: 'Refresh the folder' }))
    await waitFor(() => expect(asked.s2).toBe(2))
    expect(asked.s1).toBe(1)
  })
})
