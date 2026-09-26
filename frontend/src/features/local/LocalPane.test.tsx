import { Blob as NodeBlob } from 'node:buffer'

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { FakeDir, handleOf, offerPicker, withdrawPicker } from '@/test/fake-local'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

import { entryDragType } from '../explorer/entry-drag'

import { useLocalRoot } from './local-root-store'

/** Workspace the mock server answers for. */
const workspace = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** The saved site of the first pane. */
const prod: Site = {
  id: 's1',
  name: 'Prod',
  protocol: 'sftp',
  host: 'prod.example.com',
  port: 22,
  username: 'deploy',
  authMethod: 'password',
  listTimezone: 'UTC',
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** The remote file the tests drag. */
const remoteFile: RemoteEntry = { name: 'report.txt', path: '/report.txt', type: 'file', size: 10, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }

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

/** The drag data of a file of a site of the workspace. */
function payload(siteId: string, path: string, workspaceId = workspace): Record<string, string> {
  return { [entryDragType]: JSON.stringify({ workspaceId, siteId, path }) }
}

/** Answers the remote listing and the download of the remote file, and records uploads. */
function mockServer(download: () => Response = () => new HttpResponse('downloaded content')): string[] {
  const uploads: string[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([remoteFile])),
    http.get('/api/v1/workspaces/:id/sites/s1/files/content', () => download()),
    http.put('/api/v1/workspaces/:id/sites/s1/files/content', async ({ request }) => {
      uploads.push(new URL(request.url).searchParams.get('path') as string)
      return HttpResponse.json(remoteFile, { status: 201 })
    }),
  )
  return uploads
}

/** Opens the app with the local pane beside the first site and returns the pane. */
async function openLocalPane(path = '/sites/s1?path=%2F&split=local') {
  const view = await renderApp({ path })
  const pane = await screen.findByRole('group', { name: 'Second pane' })
  return { ...view, pane }
}

/** Picks the folder through the button of the pane. */
async function chooseFolder(user: ReturnType<typeof userEvent.setup>, pane: HTMLElement) {
  await user.click(within(pane).getByRole('button', { name: 'Choose a folder' }))
}

/**
 * Covers {@link LocalExplorer}: the pane that shows a folder of the user's computer.
 *
 * Scenarios:
 * - without a folder picker the pane says so and the selector does not offer it; with one it does, and asks to choose a folder first
 * - choosing a folder lists it, folders open and the path steps, Backspace goes up, refresh works, and a closed dialog changes nothing
 * - changing the folder shows the new one
 * - a folder that cannot be read is reported
 * - a remote file dropped on the pane is downloaded into the folder in view, with a hint while over it, and the listing shows it
 * - a name already in the folder, a refusal of the server, a failing disk and a body that never comes are reported and can be dismissed
 * - drops that carry a local file, another workspace, garbage or arrive before a folder was chosen do nothing
 * - a local file dragged onto the remote pane is uploaded, and one that cannot be read is reported
 */
describe('local folder pane', () => {
  beforeEach(() => {
    vi.stubGlobal('Blob', NodeBlob)
    useLocalRoot.setState({ root: null })
    mockSession(adminSession)
    mockWorkspaceData({ sites: [prod] })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    withdrawPicker()
  })

  it('says so when the browser cannot open folders', async () => {
    mockServer()
    const { pane } = await openLocalPane()

    expect(within(pane).getByRole('status')).toHaveTextContent('This browser cannot open folders of your computer')
    expect(within(pane).getByRole('button', { name: 'Choose a folder' })).toBeDisabled()
    expect(within(pane).getByRole('option', { name: 'Local folder' })).toBeInTheDocument()
  })

  it('offers the local folder in the selector only when the browser can', async () => {
    mockServer()
    offerPicker(new FakeDir('Documents'))
    await renderApp({ path: '/sites/s1?path=%2F&split=s1' })
    const pane = await screen.findByRole('group', { name: 'Second pane' })

    expect(await within(pane).findByRole('option', { name: 'Local folder' })).toBeInTheDocument()
    withdrawPicker()
  })

  it('lists the chosen folder and moves through it', async () => {
    mockServer()
    offerPicker(new FakeDir('Documents'))
    const root = new FakeDir('Documents').withFile('notes.txt', 'hi')
    root.withDir('photos').withFile('cat.png', 'meow')
    offerPicker(root)
    const user = userEvent.setup()
    const { history, pane } = await openLocalPane()

    expect(within(pane).getByText('Pick a folder of this computer to browse it here and drag files to and from your servers.')).toBeInTheDocument()
    await chooseFolder(user, pane)
    const grid = await within(pane).findByRole('grid')
    expect(within(grid).getByText('notes.txt')).toBeInTheDocument()
    expect(within(pane).getByText('2 items')).toBeInTheDocument()
    expect(within(grid).getByText('photos').closest('[role="row"]')).toHaveAttribute('draggable', 'false')

    await user.dblClick(within(grid).getByText('photos'))
    expect(await within(await within(pane).findByRole('grid')).findByText('cat.png')).toBeInTheDocument()
    expect(history.location.search).toContain('splitPath=%2Fphotos')
    await user.dblClick(within(within(pane).getByRole('grid')).getByText('cat.png'))
    expect(history.location.search).toContain('splitPath=%2Fphotos')

    within(pane).getByRole('grid').focus()
    await user.keyboard('{Backspace}')
    expect(await within(within(pane).getByRole('grid')).findByText('notes.txt')).toBeInTheDocument()
    root.withFile('later.txt')
    await user.click(within(pane).getByRole('button', { name: 'Refresh' }))
    expect(await within(within(pane).getByRole('grid')).findByText('later.txt')).toBeInTheDocument()
    expect(within(pane).queryByRole('menu')).not.toBeInTheDocument()
  })

  it('keeps the folder when the dialog is closed and switches when another is chosen', async () => {
    mockServer()
    const first = new FakeDir('One').withFile('one.txt')
    offerPicker(first)
    const user = userEvent.setup()
    const { pane } = await openLocalPane()
    await chooseFolder(user, pane)
    await within(pane).findByText('one.txt')

    offerPicker(null)
    await user.click(within(pane).getByRole('button', { name: 'Change folder' }))
    expect(within(pane).getByText('one.txt')).toBeInTheDocument()

    offerPicker(new FakeDir('Two').withFile('two.txt'))
    await user.click(within(pane).getByRole('button', { name: 'Change folder' }))
    expect(await within(pane).findByText('two.txt')).toBeInTheDocument()
    expect(within(pane).queryByText('one.txt')).not.toBeInTheDocument()
  })

  it('reports a folder that cannot be read and an empty one', async () => {
    mockServer()
    useLocalRoot.setState({ root: handleOf(new FakeDir('Empty')) })
    const { pane } = await openLocalPane('/sites/s1?path=%2F&split=local&splitPath=%2Fgone')

    expect(await within(pane).findByRole('alert')).toHaveTextContent('The folder could not be read.')
    const user = userEvent.setup()
    await user.click(within(pane).getByRole('button', { name: 'Up one level' }))
    expect(await within(pane).findByText('This folder is empty.')).toBeInTheDocument()
  })

  it('saves a dropped remote file in the folder in view', async () => {
    mockServer()
    const root = new FakeDir('Documents')
    offerPicker(root)
    const user = userEvent.setup()
    const { pane } = await openLocalPane()
    await chooseFolder(user, pane)
    await within(pane).findByText('This folder is empty.')
    const section = pane.querySelector('section') as HTMLElement
    const data = drag(payload('s1', '/report.txt'))

    fireEvent.dragOver(section, { dataTransfer: data })
    expect(screen.getByText('Drop here to save the file in this folder')).toBeInTheDocument()
    fireEvent.drop(section, { dataTransfer: data })

    expect(await within(pane).findByText('report.txt')).toBeInTheDocument()
    expect(screen.queryByText('Drop here to save the file in this folder')).not.toBeInTheDocument()
    expect(new TextDecoder().decode((root.children.get('report.txt') as { content: Uint8Array }).content)).toBe('downloaded content')
    fireEvent.dragOver(section, { dataTransfer: drag({ Files: '' }) })
    fireEvent.dragLeave(section)
    expect(screen.queryByText('Drop here to save the file in this folder')).not.toBeInTheDocument()
  })

  it('reports what goes wrong while saving, and lets the message go', async () => {
    let answer: () => Response = () => new HttpResponse('data')
    mockServer(() => answer())
    const root = new FakeDir('Documents').withFile('report.txt', 'mine')
    offerPicker(root)
    const user = userEvent.setup()
    const { pane } = await openLocalPane()
    await chooseFolder(user, pane)
    await within(pane).findByText('report.txt')
    const section = pane.querySelector('section') as HTMLElement
    const drop = () => fireEvent.drop(section, { dataTransfer: drag(payload('s1', '/report.txt')) })

    drop()
    expect(await within(pane).findByRole('alert')).toHaveTextContent('The file could not be saved: An item with this name already exists.')
    await user.click(within(within(pane).getByRole('alert')).getByRole('button', { name: 'Dismiss' }))
    expect(within(pane).queryByRole('alert')).not.toBeInTheDocument()

    root.children.delete('report.txt')
    answer = () => problem(404, 'path_not_found')
    drop()
    expect(await within(pane).findByRole('alert')).toHaveTextContent('The file could not be saved: This folder does not exist.')
    await user.click(within(within(pane).getByRole('alert')).getByRole('button', { name: 'Dismiss' }))

    answer = () => new HttpResponse(null, { status: 500 })
    drop()
    expect(await within(pane).findByRole('alert')).toHaveTextContent('Something went wrong')
    await user.click(within(within(pane).getByRole('alert')).getByRole('button', { name: 'Dismiss' }))

    answer = () => new HttpResponse(null, { status: 204 })
    drop()
    expect(await within(pane).findByRole('alert')).toHaveTextContent('Something went wrong')
    await user.click(within(within(pane).getByRole('alert')).getByRole('button', { name: 'Dismiss' }))

    root.failWrites = true
    answer = () => new HttpResponse('data')
    drop()
    expect(await within(pane).findByRole('alert')).toHaveTextContent('The file could not be saved: Something went wrong')
    expect(root.children.has('report.txt')).toBe(false)
  })

  it('ignores drops that are not a remote file of the workspace', async () => {
    let downloads = 0
    mockServer(() => (downloads++, new HttpResponse('x')))
    const root = new FakeDir('Documents')
    offerPicker(root)
    const user = userEvent.setup()
    const { pane } = await openLocalPane()
    const section = pane.querySelector('section') as HTMLElement

    fireEvent.drop(section, { dataTransfer: drag(payload('s1', '/report.txt')) })
    await chooseFolder(user, pane)
    await within(pane).findByText('This folder is empty.')
    fireEvent.drop(section, { dataTransfer: drag(payload('local', '/x.txt')) })
    fireEvent.drop(section, { dataTransfer: drag(payload('s1', '/report.txt', 'other')) })
    fireEvent.drop(section, { dataTransfer: drag({ [entryDragType]: 'garbage' }) })
    await new Promise((resolve) => setTimeout(resolve, 50))

    expect(downloads).toBe(0)
    expect(root.children.size).toBe(0)
  })

  it('uploads a local file dragged onto the remote pane', async () => {
    const uploads = mockServer()
    const root = new FakeDir('Documents').withFile('draft.txt', 'from my computer')
    offerPicker(root)
    const user = userEvent.setup()
    const { pane } = await openLocalPane()
    await chooseFolder(user, pane)
    const row = (await within(pane).findByText('draft.txt')).closest('[role="row"]') as HTMLElement
    const data = drag()
    const remote = screen.getAllByRole('region', { name: 'Files' })[0]

    expect(row).toHaveAttribute('draggable', 'true')
    fireEvent.dragStart(row, { dataTransfer: data })
    fireEvent.drop(remote, { dataTransfer: data })

    await waitFor(() => expect(uploads).toEqual(['/draft.txt']))
  })

  it('reports a local file that cannot be read for the upload, and does nothing without a folder', async () => {
    const uploads = mockServer()
    const root = new FakeDir('Documents')
    offerPicker(root)
    const user = userEvent.setup()
    const { pane } = await openLocalPane()
    const remote = screen.getAllByRole('region', { name: 'Files' })[0]

    fireEvent.drop(remote, { dataTransfer: drag(payload('local', '/draft.txt')) })
    await chooseFolder(user, pane)
    await within(pane).findByText('This folder is empty.')
    fireEvent.drop(remote, { dataTransfer: drag(payload('local', '/vanished.txt')) })

    expect(await within(remote).findByRole('alert')).toHaveTextContent('The copy could not start: Something went wrong. Try again.')
    expect(uploads).toEqual([])
  })
})
