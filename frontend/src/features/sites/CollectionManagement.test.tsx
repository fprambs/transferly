import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { SitesSidebar } from './SitesSidebar'

/** Collections the mock workspace holds. */
const collections = [
  { id: 'c1', name: 'Work' },
  { id: 'c2', name: 'Staging', parentId: 'c1' },
  { id: 'c3', name: 'Home' },
]

/** Renders the sidebar and waits for the tree. */
async function renderSidebar() {
  const user = userEvent.setup()
  await renderWithProviders(<SitesSidebar />)
  await screen.findByRole('treeitem', { name: /Work/ })
  return user
}

/** Opens the actions menu of a collection and picks an action. */
async function pick(user: ReturnType<typeof userEvent.setup>, collection: string, action: string) {
  await user.hover(screen.getByRole('treeitem', { name: new RegExp(collection) }))
  await user.click(screen.getByRole('button', { name: `Actions for ${collection}` }))
  await user.click(await screen.findByRole('menuitem', { name: action }))
}

/**
 * Covers collection management in the sidebar: {@link CollectionDialog}, {@link DeleteCollectionDialog} and the row keyboard support.
 *
 * Scenarios:
 * - a new collection is created at the top level or inside a chosen parent
 * - a new subcollection starts inside its parent, which opens
 * - editing renames a collection and moves it under another or to the top, without offering itself or its descendants
 * - the name is required and server errors are explained
 * - deleting asks for confirmation, and a collection that is not empty reports why
 * - rows open and close with Enter, Space and the arrow keys, ignoring other keys and inner controls
 */
describe('collection management', () => {
  beforeEach(() => {
    mockWorkspaceData({ collections })
  })

  it('creates a collection at the top level', async () => {
    let body: unknown
    server.use(
      http.post('/api/v1/workspaces/:id/collections', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ id: 'c9', name: 'New' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()

    await user.click(screen.getByRole('button', { name: 'New collection' }))
    const dialog = await screen.findByRole('dialog', { name: 'New collection' })
    await user.type(within(dialog).getByLabelText('Name'), ' New ')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(body).toEqual({ name: 'New' })
  })

  it('creates a collection inside a chosen parent', async () => {
    let body: unknown
    server.use(
      http.post('/api/v1/workspaces/:id/collections', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ id: 'c9', name: 'New' }, { status: 201 })
      }),
    )
    const user = await renderSidebar()

    await user.click(screen.getByRole('button', { name: 'New collection' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'New')
    await user.selectOptions(within(dialog).getByLabelText('Inside'), 'c3')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(body).toEqual({ name: 'New', parentId: 'c3' }))
  })

  it('starts a subcollection inside its parent and opens the parent', async () => {
    const user = await renderSidebar()

    await pick(user, 'Work', 'New subcollection')

    const dialog = await screen.findByRole('dialog', { name: 'New collection' })
    expect(within(dialog).getByLabelText('Inside')).toHaveValue('c1')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByRole('treeitem', { name: /Work/ })).toHaveAttribute('aria-expanded', 'true')
  })

  it('renames and moves a collection without offering its own subtree', async () => {
    let body: unknown
    server.use(
      http.patch('/api/v1/workspaces/:id/collections/c1', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ id: 'c1', name: 'Job', parentId: 'c3' })
      }),
    )
    const user = await renderSidebar()

    await pick(user, 'Work', 'Edit')
    const dialog = await screen.findByRole('dialog', { name: 'Edit collection' })
    expect(within(dialog).getByLabelText('Name')).toHaveValue('Work')
    expect(within(dialog).getAllByRole('option').map((option) => option.textContent)).toEqual(['Top level', 'Home'])

    await user.clear(within(dialog).getByLabelText('Name'))
    await user.type(within(dialog).getByLabelText('Name'), 'Job')
    await user.selectOptions(within(dialog).getByLabelText('Inside'), 'c3')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(body).toEqual({ name: 'Job', parentId: 'c3' }))
  })

  it('moves a nested collection to the top level', async () => {
    let body: unknown
    server.use(
      http.patch('/api/v1/workspaces/:id/collections/c2', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ id: 'c2', name: 'Staging' })
      }),
    )
    const user = await renderSidebar()
    await user.click(screen.getByRole('treeitem', { name: /Work/ }))

    await pick(user, 'Staging', 'Edit')
    const dialog = await screen.findByRole('dialog', { name: 'Edit collection' })
    expect(within(dialog).getByLabelText('Inside')).toHaveValue('c1')
    await user.selectOptions(within(dialog).getByLabelText('Inside'), '')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(body).toEqual({ name: 'Staging', moveToTop: true }))
  })

  it('requires a name and explains a server error', async () => {
    server.use(http.post('/api/v1/workspaces/:id/collections', () => problem(409, 'collection_name_taken')))
    const user = await renderSidebar()
    await user.click(screen.getByRole('button', { name: 'New collection' }))
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Create' }))
    expect(await within(dialog).findByText('Enter a name.')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Name'), 'Work')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('A collection with this name already exists here.')

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('deletes a collection after confirmation', async () => {
    let deleted = ''
    server.use(
      http.delete('/api/v1/workspaces/:id/collections/:collectionId', ({ params }) => {
        deleted = String(params.collectionId)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = await renderSidebar()

    await pick(user, 'Home', 'Delete')
    const dialog = await screen.findByRole('dialog', { name: 'Delete this collection?' })
    expect(dialog).toHaveTextContent('Home will be removed.')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(deleted).toBe('c3')
  })

  it('explains why a collection cannot be deleted and can be cancelled', async () => {
    server.use(http.delete('/api/v1/workspaces/:id/collections/:collectionId', () => problem(409, 'collection_not_empty')))
    const user = await renderSidebar()

    await pick(user, 'Work', 'Delete')
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Empty the collection first')

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('opens and closes rows from the keyboard', async () => {
    const user = await renderSidebar()
    const work = screen.getByRole('treeitem', { name: /Work/ })
    work.focus()

    await user.keyboard('x')
    expect(work).toHaveAttribute('aria-expanded', 'false')
    await user.keyboard('{ArrowLeft}')
    expect(work).toHaveAttribute('aria-expanded', 'false')
    await user.keyboard('{ArrowRight}')
    expect(work).toHaveAttribute('aria-expanded', 'true')
    await user.keyboard('{ArrowRight}')
    expect(work).toHaveAttribute('aria-expanded', 'true')
    await user.keyboard('{ArrowLeft}')
    expect(work).toHaveAttribute('aria-expanded', 'false')
    await user.keyboard('{Enter}')
    expect(work).toHaveAttribute('aria-expanded', 'true')
    await user.keyboard(' ')
    expect(work).toHaveAttribute('aria-expanded', 'false')
  })

  it('does not toggle a row when its actions button takes the key', async () => {
    const user = await renderSidebar()

    await user.hover(screen.getByRole('treeitem', { name: /Work/ }))
    screen.getByRole('button', { name: 'Actions for Work' }).focus()
    await user.keyboard('{ArrowRight}')

    expect(screen.getByRole('treeitem', { name: /Work/ })).toHaveAttribute('aria-expanded', 'false')
  })
})
