import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { useNotices } from '../notices/notices-store'

import { Explorer } from './Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

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

/** Builds an entry inside /www. */
function entry(name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `/www/${name}`, type, size: 2048, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'second' }
}

/** Records the mutations the mock server received. */
function recordMutations() {
  const seen: string[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([entry('assets', 'directory'), entry('index.html'), entry('link', 'symlink')])),
    http.post('/api/v1/workspaces/:id/sites/s1/files/directories', async ({ request }) => {
      seen.push(`mkdir ${JSON.stringify(await request.json())}`)
      return HttpResponse.json(entry('releases', 'directory'), { status: 201 })
    }),
    http.post('/api/v1/workspaces/:id/sites/s1/files/rename', async ({ request }) => {
      seen.push(`rename ${JSON.stringify(await request.json())}`)
      return HttpResponse.json(entry('static'))
    }),
    http.delete('/api/v1/workspaces/:id/sites/s1/files', ({ request }) => {
      const url = new URL(request.url)
      seen.push(`delete ${url.searchParams.get('path')} recursive=${url.searchParams.get('recursive')}`)
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return seen
}

/** Renders the explorer and waits for the table. */
async function renderExplorer() {
  const user = userEvent.setup()
  await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/www" onNavigate={vi.fn()} />)
  const grid = await screen.findByRole('grid')
  return { user, grid }
}

/**
 * Covers the file operations of the explorer: {@link EntryNameDialog}, {@link DeleteEntryDialog}, {@link Inspector} and the context menu.
 *
 * Scenarios:
 * - the inspector describes the selected entry, its type named after its format as in the table, and follows the selection, for every kind of entry
 * - a folder is created from the toolbar and from the context menu
 * - an entry is renamed from the inspector, with F2 and from the context menu, inside its own folder
 * - the new or renamed entry becomes the selected one
 * - names are validated before calling the server, and server failures are shown
 * - a file is deleted with the Delete key or the context menu and a folder only with its contents when asked to
 * - deleting can be cancelled and a failure is shown
 * - creating a folder, renaming and deleting each leave a short notice of what was done
 * - the context menu offers Open only for folders and selects the row it opens on; its actions show the key that does the same, out of
 *   their names, and Delete is drawn as destructive
 */
describe('file operations', () => {
  beforeEach(() => {
    useNotices.setState({ notices: [], next: 1 })
    mockSession(adminSession)
    mockWorkspaceData({ sites: [prod] })
  })

  it('describes the selected entry in the inspector', async () => {
    recordMutations()
    const { user, grid } = await renderExplorer()
    const inspector = screen.getByRole('complementary', { name: 'Details' })

    expect(inspector).toHaveTextContent('/www/assets')
    expect(inspector).toHaveTextContent('Folder')

    await user.click(within(grid).getByText('index.html'))
    expect(inspector).toHaveTextContent('/www/index.html')
    expect(inspector).toHaveTextContent('2.0 KB')
    expect(inspector).toHaveTextContent('HTML source file')
    expect(within(grid).getByRole('row', { name: /index\.html/ })).toHaveTextContent('HTML source file')
    expect(inspector).toHaveTextContent('Mar 8, 2026, 7:30:00 AM')
    expect(inspector).toHaveTextContent('-rw-r--r--')

    await user.click(within(grid).getByText('link'))
    expect(inspector).toHaveTextContent('Link')
  })

  it('creates a folder from the toolbar', async () => {
    const seen = recordMutations()
    const { user } = await renderExplorer()

    await user.click(screen.getByRole('button', { name: 'New folder' }))
    const dialog = await screen.findByRole('dialog', { name: 'New folder' })
    await user.type(within(dialog).getByLabelText('Name'), ' releases ')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(seen).toEqual(['mkdir {"path":"/www/releases"}'])
    expect(useNotices.getState().notices.map((notice) => notice.text)).toEqual(['Created the folder releases.'])
  })

  it('creates a folder from the context menu', async () => {
    const seen = recordMutations()
    const { user, grid } = await renderExplorer()

    fireEvent.contextMenu(within(grid).getByText('index.html'))
    await user.click(await screen.findByRole('menuitem', { name: 'New folder' }))
    const dialog = await screen.findByRole('dialog', { name: 'New folder' })
    await user.type(within(dialog).getByLabelText('Name'), 'x')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(seen).toEqual(['mkdir {"path":"/www/x"}']))
  })

  it('renames from the inspector, keeping the entry in its folder', async () => {
    const seen = recordMutations()
    const { user } = await renderExplorer()

    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'Rename' }))
    const dialog = await screen.findByRole('dialog', { name: 'Rename' })
    expect(within(dialog).getByLabelText('New name')).toHaveValue('assets')
    await user.clear(within(dialog).getByLabelText('New name'))
    await user.type(within(dialog).getByLabelText('New name'), 'static')
    await user.click(within(dialog).getByRole('button', { name: 'Rename' }))

    await waitFor(() => expect(seen).toEqual(['rename {"from":"/www/assets","to":"/www/static"}']))
    expect(useNotices.getState().notices.map((notice) => notice.text)).toEqual(['Renamed assets to static.'])
  })

  it('renames with F2 and from the context menu', async () => {
    const seen = recordMutations()
    const { user, grid } = await renderExplorer()
    grid.focus()

    await user.keyboard('{ArrowDown}{F2}')
    let dialog = await screen.findByRole('dialog', { name: 'Rename' })
    expect(within(dialog).getByLabelText('New name')).toHaveValue('index.html')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    fireEvent.contextMenu(within(grid).getByText('link'))
    await user.click(await screen.findByRole('menuitem', { name: 'Rename' }))
    dialog = await screen.findByRole('dialog', { name: 'Rename' })
    expect(within(dialog).getByLabelText('New name')).toHaveValue('link')
    await user.type(within(dialog).getByLabelText('New name'), '2')
    await user.click(within(dialog).getByRole('button', { name: 'Rename' }))

    await waitFor(() => expect(seen).toEqual(['rename {"from":"/www/link","to":"/www/link2"}']))
  })

  it('selects the entry it created or renamed', async () => {
    const seen = recordMutations()
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () =>
        HttpResponse.json([entry('assets', 'directory'), entry('index.html'), entry('releases', 'directory'), entry('static', 'directory')]),
      ),
    )
    const { user } = await renderExplorer()
    const inspector = screen.getByRole('complementary')

    await user.click(screen.getByRole('button', { name: 'New folder' }))
    await user.type(await screen.findByLabelText('Name'), 'releases')
    await user.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(inspector).toHaveTextContent('/www/releases'))

    await user.click(within(inspector).getByRole('button', { name: 'Rename' }))
    await user.type(await screen.findByLabelText('New name'), 'x')
    await user.click(screen.getByRole('button', { name: 'Rename' }))
    await waitFor(() => expect(inspector).toHaveTextContent('/www/static'))
    expect(seen).toHaveLength(2)
  })

  it('validates names and shows a server failure', async () => {
    recordMutations()
    server.use(http.post('/api/v1/workspaces/:id/sites/s1/files/directories', () => problem(409, 'path_exists')))
    const { user } = await renderExplorer()
    await user.click(screen.getByRole('button', { name: 'New folder' }))
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Create' }))
    expect(await within(dialog).findByText('Enter a name.')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Name'), 'a/b')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))
    expect(await within(dialog).findByText('The name cannot contain a slash or be . or ..')).toBeInTheDocument()

    await user.clear(within(dialog).getByLabelText('Name'))
    await user.type(within(dialog).getByLabelText('Name'), '..')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))
    expect(await within(dialog).findByText('The name cannot contain a slash or be . or ..')).toBeInTheDocument()

    await user.clear(within(dialog).getByLabelText('Name'))
    await user.type(within(dialog).getByLabelText('Name'), 'assets')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('An item with this name already exists.')
  })

  it('deletes a file with the Delete key', async () => {
    const seen = recordMutations()
    const { user, grid } = await renderExplorer()
    grid.focus()

    await user.keyboard('{ArrowDown}{Delete}')
    const dialog = await screen.findByRole('dialog', { name: 'Delete this file?' })
    expect(dialog).toHaveTextContent('index.html will be removed from the server.')
    expect(within(dialog).queryByRole('checkbox')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(seen).toEqual(['delete /www/index.html recursive=null']))
    expect(useNotices.getState().notices.map((notice) => notice.text)).toEqual(['Deleted index.html.'])
  })

  it('deletes from the context menu', async () => {
    const seen = recordMutations()
    const { user, grid } = await renderExplorer()

    fireEvent.contextMenu(within(grid).getByText('link'))
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(seen).toEqual(['delete /www/link recursive=null']))
  })

  it('deletes a folder with its contents only when asked to', async () => {
    const seen = recordMutations()
    const { user } = await renderExplorer()

    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'Delete' }))
    let dialog = await screen.findByRole('dialog', { name: 'Delete this folder?' })
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(seen).toEqual(['delete /www/assets recursive=null']))

    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'Delete' }))
    dialog = await screen.findByRole('dialog', { name: 'Delete this folder?' })
    await user.click(within(dialog).getByRole('checkbox', { name: 'Also delete everything inside it' }))
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(seen).toEqual(['delete /www/assets recursive=null', 'delete /www/assets recursive=true']))
  })

  it('shows a failed delete and can be cancelled', async () => {
    recordMutations()
    server.use(http.delete('/api/v1/workspaces/:id/sites/s1/files', () => problem(409, 'directory_not_empty')))
    const { user } = await renderExplorer()

    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The folder is not empty.')

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('offers Open only for folders in the context menu', async () => {
    recordMutations()
    const { user, grid } = await renderExplorer()
    const inspector = screen.getByRole('complementary')

    fireEvent.contextMenu(within(grid).getByText('assets'))
    expect(await screen.findByRole('menuitem', { name: 'Open' })).toBeInTheDocument()
    await user.keyboard('{Escape}')

    fireEvent.contextMenu(within(grid).getByText('index.html'))
    expect(await screen.findByRole('menuitem', { name: 'Rename' })).toHaveTextContent('F2')
    expect(screen.queryByRole('menuitem', { name: 'Open' })).not.toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toHaveAttribute('data-variant', 'destructive')
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toHaveTextContent('Del')
    expect(inspector).toHaveTextContent('/www/index.html')
  })

  it('opens a folder from the context menu', async () => {
    recordMutations()
    const onNavigate = vi.fn()
    const user = userEvent.setup()
    await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/www" onNavigate={onNavigate} />)
    const grid = await screen.findByRole('grid')

    fireEvent.contextMenu(within(grid).getByText('assets'))
    await user.click(await screen.findByRole('menuitem', { name: 'Open' }))

    expect(onNavigate).toHaveBeenCalledWith('/www/assets')
  })
})
