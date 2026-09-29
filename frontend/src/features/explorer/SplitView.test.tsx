import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { FakeDir, offerPicker, withdrawPicker } from '@/test/fake-local'
import { server } from '@/test/msw'
import { choose } from '@/test/select'
import { renderApp } from '@/test/render-app'

import { entryDragType } from './entry-drag'

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

/** Builds an entry of a folder. */
function entry(name: string, type: RemoteEntry['type'], folder: string): RemoteEntry {
  return { name, path: `${folder === '/' ? '' : folder}/${name}`, type, size: 10, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }
}

/** A stand-in for the data of a drag, which jsdom does not provide. */
function drag(initial: Record<string, string> = {}) {
  const store = new Map(Object.entries(initial))
  return {
    get types() {
      return [...store.keys()]
    },
    setData: (type: string, value: string) => void store.set(type, value),
    getData: (type: string) => store.get(type) ?? '',
    files: [] as File[],
    effectAllowed: 'none',
  }
}

/** Answers the listings of the two sites and records the copies the browser asks for. */
function mockServer(copyReply: () => Response = () => HttpResponse.json({}, { status: 202 })): unknown[] {
  const copies: unknown[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/:siteId/files', ({ params, request }) => {
      const folder = new URL(request.url).searchParams.get('path') ?? '/'
      const name = params.siteId === 's1' ? 'left.txt' : 'right.txt'
      return HttpResponse.json(folder === '/' ? [entry('docs', 'directory', '/'), entry(name, 'file', '/'), entry('shared.txt', 'file', '/')] : [entry(`in-${name}`, 'file', folder)])
    }),
    http.post('/api/v1/workspaces/:id/copies', async ({ request }) => {
      copies.push(await request.json())
      return copyReply()
    }),
  )
  return copies
}

/**
 * Covers the second pane of {@link SiteExplorer} and the drag and drop between panes of {@link Explorer}.
 *
 * Scenarios:
 * - the split button opens a second pane on the same site, the address records it, and the button closes it again
 * - the second pane changes site with its selector and moves through its own folders without touching the first pane
 * - dragging a file of one pane onto the other asks the server to copy it into the folder in view, with a hint while over it
 * - dragging a folder, or a file whose name exists in the target folder, asks what to do with files that exist there and whether to verify the copy, and the copy follows the choice or does not start when the user gives up
 * - dropping a file on its own folder, a file of another workspace or data that is not a file does nothing
 * - a copy the server refuses is explained and can be dismissed, and a later copy clears the message
 * - the compare button offers to sync the second pane against the first, only once it has something to compare against, whether the
 *   second pane is another site or a local folder that was actually chosen
 * - refusing a changed server key disconnects the site of that pane: the second pane closes, and the first leaves the site (4.13b)
 */
describe('split view', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [site('s1', 'Prod'), site('s2', 'Backup')] })
  })

  afterEach(() => withdrawPicker())

  it('opens and closes the second pane', async () => {
    mockServer()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F' })

    await screen.findAllByRole('grid')
    expect(screen.queryByRole('group', { name: 'Second pane' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Split view' }))

    const pane = await screen.findByRole('group', { name: 'Second pane' })
    expect(history.location.search).toContain('split=s1')
    expect(await within(pane).findByText('left.txt')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Split view' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(within(pane).getByRole('button', { name: 'Close the second pane' }))
    expect(screen.queryByRole('group', { name: 'Second pane' })).not.toBeInTheDocument()
    expect(history.location.search).not.toContain('split')

    await user.click(screen.getByRole('button', { name: 'Split view' }))
    await screen.findByRole('group', { name: 'Second pane' })
    await user.click(screen.getByRole('button', { name: 'Split view' }))
    expect(screen.queryByRole('group', { name: 'Second pane' })).not.toBeInTheDocument()
  })

  it('changes the site and the folder of the second pane on its own', async () => {
    mockServer()
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F&split=s1' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })

    await choose(user, within(pane).getByRole('combobox', { name: 'Site of the second pane' }), 'Backup')
    expect(await within(within(pane).getByRole('grid')).findByText('right.txt')).toBeInTheDocument()
    expect(history.location.search).toContain('split=s2')

    await user.dblClick(within(within(pane).getByRole('grid')).getByText('docs'))
    expect(await within(within(pane).getByRole('grid')).findByText('in-right.txt')).toBeInTheDocument()
    expect(history.location.search).toContain('splitPath=%2Fdocs')
    expect(history.location.search).toContain('path=%2F&')
    expect(screen.getAllByText('left.txt')).toHaveLength(1)
  })

  it('copies a file dragged from one pane into the folder of the other', async () => {
    const copies = mockServer()
    await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })
    const leftRow = (await screen.findAllByText('left.txt'))[0].closest('[role="row"]') as HTMLElement
    const data = drag()

    expect(leftRow).toHaveAttribute('draggable', 'true')
    expect((await screen.findAllByText('docs'))[0].closest('[role="row"]')).toHaveAttribute('draggable', 'true')
    fireEvent.dragStart(leftRow, { dataTransfer: data })
    const target = pane.querySelector('section') as HTMLElement
    fireEvent.dragOver(target, { dataTransfer: data })
    expect(screen.getByText('Drop here to copy the file')).toBeInTheDocument()
    fireEvent.drop(target, { dataTransfer: data })

    await waitFor(() => expect(copies).toHaveLength(1))
    expect(copies[0]).toMatchObject({ sourceSiteId: 's1', sourcePath: '/left.txt', targetSiteId: 's2', targetPath: '/left.txt' })
    expect(screen.queryByText('Drop here to copy the file')).not.toBeInTheDocument()
  })

  it('asks for a policy when a folder is dragged or the name exists, and follows the choice', async () => {
    const copies = mockServer()
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })
    await within(pane).findByText('right.txt')
    const target = pane.querySelector('section') as HTMLElement
    const rowOf = async (name: string) => (await screen.findAllByText(name))[0].closest('[role="row"]') as HTMLElement
    const dragTo = (row: HTMLElement) => {
      const data = drag()
      fireEvent.dragStart(row, { dataTransfer: data })
      fireEvent.drop(target, { dataTransfer: data })
    }

    dragTo(await rowOf('docs'))
    const dialog = await screen.findByRole('dialog', { name: 'Copy docs' })
    expect(within(dialog).getByRole('combobox', { name: 'If a file exists' })).toHaveTextContent('Skip it')
    await choose(user, within(dialog).getByRole('combobox', { name: 'If a file exists' }), 'Keep both, renaming the copy')
    await user.click(within(dialog).getByRole('checkbox', { name: /Verify the copy/ }))
    await user.click(within(dialog).getByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(copies).toHaveLength(1))
    expect(copies[0]).toMatchObject({ sourcePath: '/docs', targetPath: '/docs', policy: 'rename', verify: true })

    dragTo(await rowOf('shared.txt'))
    await user.click(within(await screen.findByRole('dialog', { name: 'Copy shared.txt' })).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(copies).toHaveLength(1)

    dragTo(await rowOf('shared.txt'))
    await user.click(within(await screen.findByRole('dialog', { name: 'Copy shared.txt' })).getByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(copies).toHaveLength(2))
    expect(copies[1]).toMatchObject({ sourcePath: '/shared.txt', policy: 'skip', verify: false })
  })

  it('ignores drops that are not a copy to another folder', async () => {
    const copies = mockServer()
    await renderApp({ path: '/sites/s1?path=%2F&split=s1' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })
    await within(pane).findByText('left.txt')
    const target = pane.querySelector('section') as HTMLElement
    const workspace = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

    fireEvent.drop(target, { dataTransfer: drag({ [entryDragType]: JSON.stringify({ workspaceId: workspace, siteId: 's1', path: '/left.txt' }) }) })
    fireEvent.drop(target, { dataTransfer: drag({ [entryDragType]: JSON.stringify({ workspaceId: 'other', siteId: 's2', path: '/x.txt' }) }) })
    fireEvent.drop(target, { dataTransfer: drag({ [entryDragType]: 'garbage' }) })
    await new Promise((resolve) => setTimeout(resolve, 50))

    expect(copies).toEqual([])
  })

  it('explains a refused copy and clears the message after the next one', async () => {
    let refuse = true
    const copies = mockServer(() => (refuse ? problem(404, 'site_not_found') : HttpResponse.json({}, { status: 202 })))
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })
    await within(pane).findByText('right.txt')
    const target = pane.querySelector('section') as HTMLElement
    const data = drag({ [entryDragType]: JSON.stringify({ workspaceId: '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10', siteId: 's1', path: '/left.txt' }) })

    fireEvent.drop(target, { dataTransfer: data })
    const alert = await within(pane).findByRole('alert')
    expect(alert).toHaveTextContent('The copy could not start: This site no longer exists.')
    await user.click(within(alert).getByRole('button', { name: 'Dismiss' }))
    expect(within(pane).queryByRole('alert')).not.toBeInTheDocument()

    fireEvent.drop(target, { dataTransfer: data })
    await within(pane).findByRole('alert')
    refuse = false
    fireEvent.drop(target, { dataTransfer: data })
    await waitFor(() => expect(within(pane).queryByRole('alert')).not.toBeInTheDocument())
    expect(copies).toHaveLength(3)
  })


  it('offers to compare the second pane against the first once there is something to compare', async () => {
    mockServer()
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })
    await within(pane).findByText('right.txt')

    await user.click(within(pane).getByRole('button', { name: 'Compare and sync with the other pane' }))
    const dialog = await screen.findByRole('dialog', { name: 'Compare and sync' })
    expect(within(dialog).getByText('Files that differ between Prod and Backup, or exist only on one side.')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog', { name: 'Compare and sync' })).not.toBeInTheDocument()
  })

  it('offers to compare only once a local folder was actually chosen for the second pane', async () => {
    mockServer()
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2F&split=local' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })

    expect(within(pane).queryByRole('button', { name: 'Compare and sync with the other pane' })).not.toBeInTheDocument()
    offerPicker(new FakeDir('Documents'))
    await user.click(within(pane).getByRole('button', { name: 'Choose a folder' }))
    await user.click(await within(pane).findByRole('button', { name: 'Compare and sync with the other pane' }))

    expect(await screen.findByText('Files that differ between Prod and Local folder, or exist only on one side.')).toBeInTheDocument()
  })

  it('disconnects the site of the pane whose changed server key the user refuses', async () => {
    const disconnected: string[] = []
    server.use(
      http.get('/api/v1/workspaces/:id/sites/:siteId/files', () =>
        HttpResponse.json(
          { type: 'about:blank', title: 'x', status: 409, code: 'host_key_changed', params: { keyType: 'ssh-ed25519', fingerprint: 'SHA256:new', trusted: ['SHA256:old'] } },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
      http.delete('/api/v1/workspaces/:id/sites/:siteId/connection', ({ params }) => {
        disconnected.push(params.siteId as string)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    const { history } = await renderApp({ path: '/sites/s1?path=%2F&split=s2' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })

    await user.click(await within(pane).findByRole('button', { name: 'Cancel and disconnect' }))
    await waitFor(() => expect(history.location.search).not.toContain('split'))
    await user.click(screen.getByRole('button', { name: 'Cancel and disconnect' }))
    await waitFor(() => expect(history.location.pathname).toBe('/'))
    expect(disconnected).toEqual(['s2', 's1'])
  })
})
