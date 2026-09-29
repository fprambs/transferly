import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import type { Collection, Site } from '@/lib/api'
import { mockWorkspaceData, personalWorkspace, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { SitesSidebar } from './SitesSidebar'

/** Builds a saved site, inside a collection when given. */
function site(id: string, name: string, collectionId?: string): Site {
  return {
    id,
    name,
    collectionId,
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

/** Collections: Clients holds Acme; Archive is at the top. */
const collections: Collection[] = [
  { id: 'c1', name: 'Clients' },
  { id: 'c2', name: 'Acme', parentId: 'c1' },
  { id: 'c3', name: 'Archive' },
]

/** A stand-in for the data of a drag, which jsdom does not provide. */
function transfer() {
  const store = new Map<string, string>()
  return {
    get types() {
      return [...store.keys()]
    },
    setData: (type: string, value: string) => void store.set(type, value),
    getData: (type: string) => store.get(type) ?? '',
    effectAllowed: 'none',
    dropEffect: 'none',
  }
}

/** Records the moves the server receives. */
function recordMoves(): string[] {
  const moves: string[] = []
  server.use(
    http.post('/api/v1/workspaces/:id/sites/:siteId/move', async ({ params, request }) => {
      const body = (await request.json()) as { collectionId?: string }
      moves.push(`site ${params.siteId as string} -> ${body.collectionId ?? 'top'}`)
      return HttpResponse.json(site(params.siteId as string, 'x', body.collectionId))
    }),
    http.patch('/api/v1/workspaces/:id/collections/:collectionId', async ({ params, request }) => {
      const body = (await request.json()) as { parentId?: string; moveToTop?: boolean }
      moves.push(`collection ${params.collectionId as string} -> ${body.moveToTop === true ? 'top' : (body.parentId as string)}`)
      return HttpResponse.json({ id: params.collectionId, name: 'x' })
    }),
  )
  return moves
}

/** Drags a row and drops it on an element, as a browser does. */
function dragTo(row: HTMLElement, target: HTMLElement): void {
  const data = transfer()
  fireEvent.dragStart(row, { dataTransfer: data })
  fireEvent.dragOver(target, { dataTransfer: data })
  fireEvent.drop(target, { dataTransfer: data })
  fireEvent.dragEnd(row, { dataTransfer: data })
}

/** Renders the sidebar with the collections open and returns the tree. */
async function renderTree() {
  await renderWithProviders(<SitesSidebar onOpen={() => undefined} />)
  const tree = await screen.findByRole('tree', { name: 'Sites' })
  fireEvent.click(await within(tree).findByRole('treeitem', { name: /Clients/ }))
  fireEvent.click(await within(tree).findByRole('treeitem', { name: /Acme/ }))
  return tree
}

/**
 * Covers moving sites and collections by dragging them in the sites tree (4.14h).
 *
 * Scenarios:
 * - a site dropped on a collection, or on a site inside it, moves into that collection, which opens; dropped on the empty room of the tree
 *   it moves to the top level
 * - a collection moves into another collection or to the top level, but never into itself or one of its own collections
 * - the collection under the pointer, or the whole tree for the top level, is marked while a drop there is allowed, and the mark goes
 *   when the pointer leaves the tree
 * - a move the server refuses is explained
 * - whoever may only browse the workspace cannot drag
 */
describe('sites tree drag', () => {
  it('moves a site into a collection or to the top level', async () => {
    mockWorkspaceData({ collections, sites: [site('s1', 'Prod'), site('s2', 'Web', 'c2')] })
    const moves = recordMoves()
    const tree = await renderTree()

    dragTo(within(tree).getByRole('treeitem', { name: /Prod/ }), within(tree).getByRole('treeitem', { name: /Archive/ }))
    await waitFor(() => expect(moves).toEqual(['site s1 -> c3']))
    await waitFor(() => expect(within(tree).getByRole('treeitem', { name: /Archive/ })).toHaveAttribute('aria-expanded', 'true'))

    dragTo(within(tree).getByRole('treeitem', { name: /Prod/ }), within(tree).getByRole('treeitem', { name: /Web/ }))
    dragTo(within(tree).getByRole('treeitem', { name: /Web/ }), tree)
    await waitFor(() => expect(moves).toEqual(['site s1 -> c3', 'site s1 -> c2', 'site s2 -> top']))
  })

  it('moves collections, never into themselves', async () => {
    mockWorkspaceData({ collections, sites: [] })
    const moves = recordMoves()
    const tree = await renderTree()
    const clients = within(tree).getByRole('treeitem', { name: /Clients/ })

    dragTo(clients, within(tree).getByRole('treeitem', { name: /Acme/ }))
    dragTo(clients, clients)
    dragTo(clients, tree)
    dragTo(within(tree).getByRole('treeitem', { name: /Acme/ }), within(tree).getByRole('treeitem', { name: /Archive/ }))
    dragTo(within(tree).getByRole('treeitem', { name: /Acme/ }), tree)

    await waitFor(() => expect(moves).toEqual(['collection c2 -> c3', 'collection c2 -> top']))
  })

  it('marks where the row would go while it is allowed', async () => {
    mockWorkspaceData({ collections, sites: [site('s1', 'Prod', 'c1')] })
    const tree = await renderTree()
    const data = transfer()
    const archive = within(tree).getByRole('treeitem', { name: /Archive/ })

    fireEvent.dragStart(within(tree).getByRole('treeitem', { name: /Prod/ }), { dataTransfer: data })
    fireEvent.dragOver(archive, { dataTransfer: data })
    expect(archive).toHaveClass('ring-2')
    fireEvent.dragOver(tree, { dataTransfer: data })
    expect(archive).not.toHaveClass('ring-2')
    expect(tree).toHaveClass('ring-inset')
    fireEvent.dragOver(within(tree).getByRole('treeitem', { name: /Clients/ }), { dataTransfer: data })
    expect(tree).not.toHaveClass('ring-inset')
    fireEvent.dragOver(tree, { dataTransfer: data })
    fireEvent.dragLeave(tree, { relatedTarget: document.body })
    expect(tree).not.toHaveClass('ring-inset')
    fireEvent.dragEnd(within(tree).getByRole('treeitem', { name: /Prod/ }), { dataTransfer: data })
    fireEvent.dragOver(archive, { dataTransfer: data })
    expect(archive).not.toHaveClass('ring-2')
  })

  it('explains a move the server refuses', async () => {
    mockWorkspaceData({ collections: [...collections, { id: 'c4', name: 'Acme', parentId: 'c3' }], sites: [] })
    server.use(http.patch('/api/v1/workspaces/:id/collections/:collectionId', () => problem(409, 'collection_name_taken')))
    const tree = await renderTree()

    dragTo(within(tree).getAllByRole('treeitem', { name: /Acme/ })[0], within(tree).getByRole('treeitem', { name: /Archive/ }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/already/)
  })

  it('does not let a viewer drag', async () => {
    mockWorkspaceData({ collections, sites: [site('s1', 'Prod')], workspaces: [{ ...personalWorkspace, role: 'viewer' }] })
    const tree = await renderTree()

    expect(within(tree).getByRole('treeitem', { name: /Prod/ })).toHaveAttribute('draggable', 'false')
    expect(within(tree).getByRole('treeitem', { name: /Archive/ })).toHaveAttribute('draggable', 'false')
  })
})
