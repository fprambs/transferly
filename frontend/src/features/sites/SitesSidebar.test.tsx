import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { QuickConnection, Site } from '@/lib/api'
import { mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { renderWithProviders } from '@/test/render'

import { SitesSidebar } from './SitesSidebar'

/** Builds a saved site. */
function site(id: string, name: string, collectionId?: string): Site {
  return {
    connector: 'sftp', config: { host: `${id}.example.com`, port: '22', username: 'u', auth: 'password', list_timezone: 'UTC', charset: 'utf-8' },
    id,
    name,
    collectionId,
    speedLimitBps: 0,
    secrets: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

/**
 * Covers {@link SitesSidebar}.
 *
 * Scenarios:
 * - a workspace without sites shows the empty message, with buttons to add a site or import them for whoever may manage sites
 * - collections, sites and quick connections appear as a tree with counts and the "not saved" mark
 * - opening a collection reveals its children and closing it hides them again; choosing in its menu does not open or close it
 * - clicking a row selects it and only one row is selected at a time; Space selects the row with focus
 * - a double click or Enter connects a site or a quick connection, Ctrl with the double click opens it in a new tab, and a click only selects (4.14f)
 * - the toolbar offers quick connect and the create actions
 * - the labels follow the interface language
 * - a site wears its color label and the icon of its connector, with a badge when its connection may travel unencrypted; a connector
 *   the catalog does not list gets the generic icon
 * - connected sites and quick connections carry a green dot named for screen readers, and connected sites offer to disconnect in their menu
 */
describe('SitesSidebar', () => {
  it('marks the connected sites in green and offers to disconnect them', async () => {
    const quick: QuickConnection = { connector: 'sftp', config: { host: 'q.example.com', port: '22', username: 'u', auth: 'password', list_timezone: 'UTC', charset: 'utf-8' }, id: 'q1', name: 'q.example.com', createdAt: '2026-01-01T00:00:00Z' }
    mockWorkspaceData({ sites: [site('s1', 'Prod'), site('s2', 'Stage')], quick: [quick], connected: ['s1', 'q1'] })
    const disconnected: string[] = []
    const user = userEvent.setup()
    await renderWithProviders(<SitesSidebar onOpen={() => undefined} onDisconnect={(id) => disconnected.push(id)} />)

    const prod = await screen.findByRole('treeitem', { name: /Prod/ })
    await within(prod).findByText('Connected')
    expect(within(screen.getByRole('treeitem', { name: /q\.example\.com/ })).getByText('Connected')).toBeInTheDocument()
    expect(within(screen.getByRole('treeitem', { name: /Stage/ })).queryByText('Connected')).not.toBeInTheDocument()

    await user.click(within(prod).getByRole('button', { name: 'Actions for Prod' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Disconnect' }))
    expect(disconnected).toEqual(['s1'])

    await user.click(within(screen.getByRole('treeitem', { name: /Stage/ })).getByRole('button', { name: 'Actions for Stage' }))
    await screen.findByRole('menuitem', { name: 'Edit' })
    expect(screen.queryByRole('menuitem', { name: 'Disconnect' })).not.toBeInTheDocument()
  })

  it('draws the color label of a site and the icon of its connector, with the badge of its transport', async () => {
    mockWorkspaceData({ sites: [{ ...site('s1', 'Prod'), color: 'green' }, { ...site('s2', 'Legacy'), connector: 'ftp', config: { host: 'legacy.example.com', encryption: 'plain' } }] })
    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)

    const prod = await screen.findByRole('treeitem', { name: /Prod/ })
    expect(prod.querySelector('[data-color="green"]')).toHaveClass('bg-(--label-green)')
    expect(prod.querySelector('[data-transport]')).toHaveAttribute('data-transport', 'sftp')
    const legacy = screen.getByRole('treeitem', { name: /Legacy/ })
    expect(legacy.querySelector('[data-color]')).toBeNull()
    expect(legacy.querySelector('[data-transport]')).toHaveAttribute('data-transport', 'plain')
    await waitFor(() => expect(legacy.querySelector('[data-icon]')).toHaveAttribute('data-icon', 'ftps'))
    expect(prod.querySelector('[data-icon]')).toHaveAttribute('data-icon', 'sftp')
  })

  it('draws the generic connector icon for a site whose connector the catalog does not list', async () => {
    mockWorkspaceData({ sites: [{ ...site('s1', 'Shares'), connector: 'webdav', config: { host: 'dav.example.com' } }] })
    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)

    const row = await screen.findByRole('treeitem', { name: /Shares/ })
    expect(row.querySelector('[data-icon]')).toHaveAttribute('data-icon', 'connector')
  })

  it('offers no way to disconnect when none is given', async () => {
    mockWorkspaceData({ sites: [site('s1', 'Prod')], connected: ['s1'] })
    const user = userEvent.setup()
    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)
    const prod = await screen.findByRole('treeitem', { name: /Prod/ })
    await within(prod).findByText('Connected')
    await user.click(within(prod).getByRole('button', { name: 'Actions for Prod' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Disconnect' }))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('shows the empty message for a workspace without sites, with ways to add the first ones', async () => {
    mockWorkspaceData()
    const user = userEvent.setup()

    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)

    expect(await screen.findByText('No sites yet')).toBeInTheDocument()
    await user.click(await screen.findByRole('button', { name: 'Add a site' }))
    expect(await screen.findByRole('dialog', { name: 'New site' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: 'Import from FileZilla' }))
    expect(await screen.findByRole('dialog', { name: 'Import sites' })).toBeInTheDocument()
  })

  it('leaves the ways to add sites out of the empty message for a viewer', async () => {
    mockWorkspaceData({ workspaces: [{ ...personalWorkspace, role: 'viewer' }] })

    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)

    expect(await screen.findByText('No sites yet')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add a site' })).not.toBeInTheDocument()
  })

  it('draws the tree and opens and closes collections', async () => {
    mockWorkspaceData({
      collections: [{ id: 'c1', name: 'Work' }],
      sites: [site('s1', 'Staging', 'c1'), site('s2', 'Home')],
      quick: [
        {
          connector: 'sftp', config: { host: '198.51.100.7', port: '22', username: 'deploy', auth: 'password', list_timezone: 'UTC', charset: 'utf-8' },
          id: 'q1',
          name: '198.51.100.7',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    const user = userEvent.setup()
    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)
    const tree = await screen.findByRole('tree', { name: 'Sites' })

    const work = await within(tree).findByRole('treeitem', { name: /Work/ })
    expect(work).toHaveAttribute('aria-expanded', 'false')
    expect(within(work).getByText('1')).toBeInTheDocument()
    expect(within(tree).getByText('not saved')).toBeInTheDocument()
    expect(within(tree).getByText('Home')).toBeInTheDocument()
    expect(within(tree).queryByText('Staging')).not.toBeInTheDocument()

    await user.click(work)
    expect(work).toHaveAttribute('aria-expanded', 'true')
    expect(within(tree).getByText('Staging')).toBeInTheDocument()

    await user.click(work)
    expect(within(tree).queryByText('Staging')).not.toBeInTheDocument()

    await user.click(within(work).getByRole('button', { name: /Work/ }))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }))
    expect(work).toHaveAttribute('aria-expanded', 'false')
  })

  it('selects one row at a time', async () => {
    mockWorkspaceData({
      collections: [{ id: 'c1', name: 'Work' }],
      sites: [site('s2', 'Home')],
      quick: [
        {
          connector: 'sftp', config: { host: '198.51.100.7', port: '22', username: 'deploy', auth: 'password', list_timezone: 'UTC', charset: 'utf-8' },
          id: 'q1',
          name: '198.51.100.7',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    const user = userEvent.setup()
    const onOpen = vi.fn()
    await renderWithProviders(<SitesSidebar onOpen={onOpen} />)
    const home = await screen.findByRole('treeitem', { name: /Home/ })
    const work = screen.getByRole('treeitem', { name: /Work/ })
    const quick = screen.getByRole('treeitem', { name: /198.51.100.7/ })

    await user.click(home)
    expect(home).toHaveAttribute('aria-selected', 'true')
    expect(work).toHaveAttribute('aria-selected', 'false')

    await user.click(work)
    expect(work).toHaveAttribute('aria-selected', 'true')
    expect(home).toHaveAttribute('aria-selected', 'false')

    quick.focus()
    await user.keyboard('x')
    expect(quick).toHaveAttribute('aria-selected', 'false')
    await user.keyboard('{Enter}')
    expect(quick).toHaveAttribute('aria-selected', 'true')

    home.focus()
    await user.keyboard(' ')
    expect(home).toHaveAttribute('aria-selected', 'true')
    expect(quick).toHaveAttribute('aria-selected', 'false')
    expect(onOpen.mock.calls).toEqual([['q1', false]])

    await user.dblClick(home)
    await user.keyboard('{Control>}')
    await user.dblClick(quick)
    await user.keyboard('{/Control}')
    expect(onOpen.mock.calls).toEqual([['q1', false], ['s2', false], ['q1', true]])

    await user.click(within(home).getByRole('button', { name: 'Actions for Home' }))
    const item = await screen.findByRole('menuitem', { name: 'Open in a new tab' })
    fireEvent.doubleClick(item)
    expect(onOpen.mock.calls).toHaveLength(3)
    await user.click(item)
    expect(onOpen.mock.calls.slice(3)).toEqual([['s2', true]])
  })

  it('offers quick connect and the create actions', async () => {
    mockWorkspaceData()

    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)

    expect(await screen.findByRole('button', { name: /Quick connect/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New site' })).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: 'More site actions' }))
    expect(await screen.findByRole('menuitem', { name: 'New collection' })).toBeInTheDocument()
  })

  it('speaks the interface language', async () => {
    mockWorkspaceData()

    await renderWithProviders(<SitesSidebar onOpen={() => undefined} />, 'es')

    expect(await screen.findByRole('navigation', { name: 'Sitios' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Conexión rápida/ })).toBeInTheDocument()
  })
})
