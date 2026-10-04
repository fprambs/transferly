import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Bookmark, RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, personalWorkspace, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { Explorer } from '../explorer/Explorer'

/** A saved site the explorer browses. */
const prod: Site = {
  connector: 'sftp', config: { host: 'prod.example.com', port: '22', username: 'deploy', auth: 'password', list_timezone: 'UTC', charset: 'utf-8' },
  id: 's1',
  name: 'Prod',
  speedLimitBps: 0,
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** Builds a bookmark. */
function mark(id: string, name: string, path: string): Bookmark {
  return { id, name, path, createdAt: '2026-09-29T10:00:00Z' }
}

/** A mock server that keeps the bookmarks of the site and records the writes it receives. */
function mockBookmarks(initial: Bookmark[]) {
  let items = [...initial]
  const writes: Array<{ method: string; body?: unknown }> = []
  const entry: RemoteEntry = { name: 'a.txt', path: '/www/a.txt', type: 'file', size: 1, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }
  server.use(
    http.get('/api/v1/workspaces/:id/sites/:siteId/files', () => HttpResponse.json([entry])),
    http.get('/api/v1/workspaces/:id/sites/s1/bookmarks', () => HttpResponse.json(items)),
    http.post('/api/v1/workspaces/:id/sites/s1/bookmarks', async ({ request }) => {
      const body = (await request.json()) as { name: string; path: string }
      writes.push({ method: 'POST', body })
      const created = mark(`b${items.length + 1}`, body.name, body.path)
      items = [...items, created]
      return HttpResponse.json(created, { status: 201 })
    }),
    http.patch('/api/v1/workspaces/:id/sites/s1/bookmarks/:bookmarkId', async ({ request, params }) => {
      const body = (await request.json()) as { name: string; path: string }
      writes.push({ method: 'PATCH', body })
      items = items.map((item) => (item.id === params.bookmarkId ? { ...item, ...body } : item))
      return HttpResponse.json(items.find((item) => item.id === params.bookmarkId))
    }),
    http.delete('/api/v1/workspaces/:id/sites/s1/bookmarks/:bookmarkId', ({ params }) => {
      writes.push({ method: 'DELETE', body: params.bookmarkId })
      items = items.filter((item) => item.id !== params.bookmarkId)
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return writes
}

/** Renders the explorer of a site at a path. */
async function renderExplorer(path = '/www', siteId = 's1') {
  const onNavigate = vi.fn()
  const user = userEvent.setup()
  await renderWithProviders(<Explorer workspaceId={personalWorkspace.id} siteId={siteId} path={path} onNavigate={onNavigate} />)
  await screen.findByRole('grid')
  return { user, onNavigate }
}

/** Opens the bookmarks menu. */
async function openMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Bookmarks' }))
  return screen.findByRole('menu')
}

/**
 * Covers the bookmarks of saved sites in the explorer (4.14b): {@link BookmarksMenu}, {@link BookmarkDialog} and {@link ManageBookmarksDialog}.
 *
 * Scenarios:
 * - the menu lists the bookmarks of the site with their folders and opens the chosen one; the icon is filled while the folder in view is
 *   bookmarked, and bookmarking it again is off
 * - a site without bookmarks says so; bookmarking the folder in view suggests its name, or the site's at the root, and saves it
 * - the form asks for a name of up to 100 characters and an absolute folder; a refusal of the server is explained, an unknown one too
 * - managing lists the bookmarks, edits one and goes back to the list, removes one, explains a failed removal, says when none is left and
 *   closes
 * - whoever may only browse the workspace opens bookmarks but cannot add or manage them, and a quick connection has no bookmarks
 * - the bookmarks bar shows a chip per bookmark, marks the one of the folder in view and opens a chip; it is left out without bookmarks
 * - chips that do not fit go to a button that lists and opens them
 */
describe('bookmarks', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [prod] })
  })

  it('lists the bookmarks and opens the chosen one', async () => {
    mockBookmarks([mark('b1', 'Web root', '/www'), mark('b2', 'Logs', '/var/log')])
    const { user, onNavigate } = await renderExplorer()

    await waitFor(() => expect(screen.getByRole('button', { name: 'Bookmarks' }).querySelector('svg')).toHaveClass('fill-warn'))
    const menu = await openMenu(user)
    expect(within(menu).getByRole('menuitem', { name: /Web root/ })).toHaveTextContent('/www')
    expect(within(menu).getByRole('menuitem', { name: 'Bookmark this folder' })).toHaveAttribute('aria-disabled', 'true')
    await user.click(within(menu).getByRole('menuitem', { name: /Logs/ }))

    expect(onNavigate).toHaveBeenCalledWith('/var/log')
  })

  it('bookmarks the folder in view with its name suggested', async () => {
    const writes = mockBookmarks([])
    const { user } = await renderExplorer('/www/html')

    const menu = await openMenu(user)
    expect(within(menu).getByText('No bookmarks on this site yet.')).toBeInTheDocument()
    expect(within(menu).getByRole('menuitem', { name: 'Manage bookmarks…' })).toHaveAttribute('aria-disabled', 'true')
    await user.click(within(menu).getByRole('menuitem', { name: 'Bookmark this folder' }))
    const dialog = await screen.findByRole('dialog', { name: 'Bookmark this folder' })
    expect(within(dialog).getByLabelText('Name')).toHaveValue('html')
    expect(within(dialog).getByLabelText('Folder')).toHaveValue('/www/html')
    await user.clear(within(dialog).getByLabelText('Name'))
    await user.type(within(dialog).getByLabelText('Name'), 'Site files')
    await user.click(within(dialog).getByRole('button', { name: 'Add bookmark' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(writes).toEqual([{ method: 'POST', body: { name: 'Site files', path: '/www/html' } }])
    await waitFor(() => expect(screen.getByRole('button', { name: 'Bookmarks' }).querySelector('svg')).toHaveClass('fill-warn'))
  })

  it('suggests the name of the site for its root', async () => {
    mockBookmarks([])
    const { user } = await renderExplorer('/')

    await user.click(within(await openMenu(user)).getByRole('menuitem', { name: 'Bookmark this folder' }))

    expect(within(await screen.findByRole('dialog')).getByLabelText('Name')).toHaveValue('Prod')
  })

  it('checks the form and explains a refusal of the server', async () => {
    let code = 'bookmark_name_taken'
    mockBookmarks([])
    server.use(http.post('/api/v1/workspaces/:id/sites/s1/bookmarks', () => problem(409, code)))
    const { user } = await renderExplorer()
    await user.click(within(await openMenu(user)).getByRole('menuitem', { name: 'Bookmark this folder' }))
    const dialog = await screen.findByRole('dialog')
    const name = within(dialog).getByLabelText('Name')
    const folder = within(dialog).getByLabelText('Folder')
    const submit = within(dialog).getByRole('button', { name: 'Add bookmark' })

    await user.clear(name)
    await user.clear(folder)
    await user.type(folder, 'relative')
    await user.click(submit)
    expect(await within(dialog).findByText('Give the bookmark a name.')).toBeInTheDocument()
    expect(within(dialog).getByText('The folder must start with /.')).toBeInTheDocument()
    await user.type(name, 'x'.repeat(101))
    await user.click(submit)
    expect(await within(dialog).findByText('Use up to 100 characters.')).toBeInTheDocument()

    await user.clear(name)
    await user.type(name, 'Logs')
    await user.clear(folder)
    await user.type(folder, '/var/log')
    await user.click(submit)
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('This site already has a bookmark with that name.')
    code = 'boom'
    await user.click(submit)
    await waitFor(() => expect(within(dialog).getByRole('alert')).toHaveTextContent('Something went wrong. Try again.'))
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('edits and removes bookmarks from the manage dialog', async () => {
    const writes = mockBookmarks([mark('b1', 'Logs', '/var/log'), mark('b2', 'Home', '/home')])
    const { user } = await renderExplorer()

    await user.click(within(await openMenu(user)).getByRole('menuitem', { name: 'Manage bookmarks…' }))
    let dialog = await screen.findByRole('dialog', { name: 'Manage bookmarks' })
    expect(within(dialog).getByText('Rename, move or remove the bookmarks of Prod.')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Edit Logs' }))
    const edit = await screen.findByRole('dialog', { name: 'Edit bookmark' })
    expect(within(edit).getByLabelText('Folder')).toHaveValue('/var/log')
    await user.clear(within(edit).getByLabelText('Folder'))
    await user.type(within(edit).getByLabelText('Folder'), '/var/log/nginx')
    await user.click(within(edit).getByRole('button', { name: 'Save' }))

    dialog = await screen.findByRole('dialog', { name: 'Manage bookmarks' })
    expect(await within(dialog).findByText('/var/log/nginx')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Remove Home' }))
    await waitFor(() => expect(within(dialog).queryByText('Home')).not.toBeInTheDocument())
    await user.click(within(dialog).getByRole('button', { name: 'Remove Logs' }))
    expect(await within(dialog).findByText('No bookmarks on this site yet.')).toBeInTheDocument()
    expect(writes).toEqual([
      { method: 'PATCH', body: { name: 'Logs', path: '/var/log/nginx' } },
      { method: 'DELETE', body: 'b2' },
      { method: 'DELETE', body: 'b1' },
    ])
    await user.click(within(dialog).getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('explains a removal that fails', async () => {
    mockBookmarks([mark('b1', 'Logs', '/var/log')])
    server.use(http.delete('/api/v1/workspaces/:id/sites/s1/bookmarks/:bookmarkId', () => problem(403, 'forbidden')))
    const { user } = await renderExplorer()

    await user.click(within(await openMenu(user)).getByRole('menuitem', { name: 'Manage bookmarks…' }))
    const dialog = await screen.findByRole('dialog', { name: 'Manage bookmarks' })
    await user.click(within(dialog).getByRole('button', { name: 'Remove Logs' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('You do not have permission to change the bookmarks of this workspace.')
    expect(within(dialog).getByText('Logs')).toBeInTheDocument()
  })

  it('lets a viewer open bookmarks but not change them', async () => {
    mockWorkspaceData({ sites: [prod], workspaces: [{ ...personalWorkspace, role: 'viewer' }] })
    mockBookmarks([mark('b1', 'Logs', '/var/log')])
    const { user } = await renderExplorer()

    const menu = await openMenu(user)
    expect(within(menu).getByRole('menuitem', { name: /Logs/ })).toBeInTheDocument()
    expect(within(menu).queryByRole('menuitem', { name: 'Bookmark this folder' })).not.toBeInTheDocument()
    expect(within(menu).queryByRole('menuitem', { name: 'Manage bookmarks…' })).not.toBeInTheDocument()
  })

  it('shows the bookmarks as chips and opens one', async () => {
    mockBookmarks([mark('b1', 'Web root', '/www'), mark('b2', 'Logs', '/var/log')])
    const { user, onNavigate } = await renderExplorer()

    const bar = await screen.findByRole('navigation', { name: 'Bookmarks bar' })
    expect(await within(bar).findByRole('button', { name: 'Web root' })).toHaveAttribute('aria-current', 'location')
    expect(within(bar).getByRole('button', { name: 'Logs' })).not.toHaveAttribute('aria-current')
    expect(within(bar).queryByRole('button', { name: /more bookmark/ })).not.toBeInTheDocument()
    await user.click(within(bar).getByRole('button', { name: 'Logs' }))

    expect(onNavigate).toHaveBeenCalledWith('/var/log')
  })

  it('leaves the bar out without bookmarks', async () => {
    mockBookmarks([])
    await renderExplorer()

    expect(screen.queryByRole('navigation', { name: 'Bookmarks bar' })).not.toBeInTheDocument()
  })

  it('moves the chips that do not fit to a button', async () => {
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(100)
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(250)
    mockBookmarks([mark('b1', 'One', '/1'), mark('b2', 'Two', '/2'), mark('b3', 'Three', '/3')])
    const { user, onNavigate } = await renderExplorer()

    const bar = await screen.findByRole('navigation', { name: 'Bookmarks bar' })
    await within(bar).findByRole('button', { name: 'Two' })
    expect(within(bar).queryByRole('button', { name: 'Three' })).not.toBeInTheDocument()
    await user.click(within(bar).getByRole('button', { name: '1 more bookmark' }))
    await user.click(await screen.findByRole('menuitem', { name: /Three/ }))

    expect(onNavigate).toHaveBeenCalledWith('/3')
    vi.restoreAllMocks()
  })

  it('offers no bookmarks on a quick connection', async () => {
    mockBookmarks([])
    await renderExplorer('/www', 'q1')

    expect(screen.queryByRole('button', { name: 'Bookmarks' })).not.toBeInTheDocument()
  })
})
