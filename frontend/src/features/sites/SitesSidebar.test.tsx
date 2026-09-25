import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'
import { mockWorkspaceData } from '@/test/handlers'
import { renderWithProviders } from '@/test/render'

import { SitesSidebar } from './SitesSidebar'

/** Builds a saved site. */
function site(id: string, name: string, collectionId?: string): Site {
  return {
    id,
    name,
    collectionId,
    protocol: 'sftp',
    host: `${id}.example.com`,
    port: 22,
    username: 'u',
    authMethod: 'password',
    listTimezone: 'UTC',
    secrets: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

/**
 * Covers {@link SitesSidebar}.
 *
 * Scenarios:
 * - a workspace without sites shows the empty message
 * - collections, sites and quick connections appear as a tree with counts and the "not saved" mark
 * - opening a collection reveals its children and closing it hides them again
 * - the toolbar offers quick connect and the create actions
 * - the labels follow the interface language
 */
describe('SitesSidebar', () => {
  it('shows the empty message for a workspace without sites', async () => {
    mockWorkspaceData()

    await renderWithProviders(<SitesSidebar />)

    expect(await screen.findByText('No sites yet')).toBeInTheDocument()
  })

  it('draws the tree and opens and closes collections', async () => {
    mockWorkspaceData({
      collections: [{ id: 'c1', name: 'Work' }],
      sites: [site('s1', 'Staging', 'c1'), site('s2', 'Home')],
      quick: [
        {
          id: 'q1',
          name: '198.51.100.7',
          protocol: 'sftp',
          host: '198.51.100.7',
          port: 22,
          username: 'deploy',
          authMethod: 'password',
          listTimezone: 'UTC',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    const user = userEvent.setup()
    await renderWithProviders(<SitesSidebar />)
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
  })

  it('offers quick connect and the create actions', async () => {
    mockWorkspaceData()

    await renderWithProviders(<SitesSidebar />)

    expect(await screen.findByRole('button', { name: /Quick connect/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New collection' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New site' })).toBeInTheDocument()
  })

  it('speaks the interface language', async () => {
    mockWorkspaceData()

    await renderWithProviders(<SitesSidebar />, 'es')

    expect(await screen.findByRole('navigation', { name: 'Sitios' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Conexión rápida/ })).toBeInTheDocument()
  })
})
