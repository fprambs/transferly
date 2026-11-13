import { Blob as NodeBlob } from 'node:buffer'

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose as chooseOption, optionsOf } from '@/test/select'
import { renderWithProviders } from '@/test/render'

import { maxTreeEntries } from '../local/tree'

import { Explorer } from './Explorer'

vi.mock('./free-name', async (actual) => {
  const real = await actual<typeof import('./free-name')>()
  return { ...real, freeName: (exists: (candidate: string) => Promise<boolean>, target: string) => real.freeName(exists, target, 3) }
})

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** A remote entry. */
function entry(name: string): RemoteEntry {
  return { name, path: `/www/${name}`, type: 'file', size: 1, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'second' }
}

/** What the mock server saw, in order. */
type Call = `mkdir ${string}` | `put ${string} ${string}`

/** Options of {@link mockServer}. */
interface Behavior {
  /** Remote paths that already exist. */
  existing?: string[]
  /** Paths whose creation is refused with this code. */
  refuse?: Record<string, string>
}

/** Answers listings, folder creation, stats and uploads, and records what changed. */
function mockServer({ existing = [], refuse = {} }: Behavior = {}): Call[] {
  const calls: Call[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([entry('existing.txt')])),
    http.get('/api/v1/workspaces/:id/sites/s1/files/entry', ({ request }) => {
      const path = new URL(request.url).searchParams.get('path') as string
      return existing.includes(path) ? HttpResponse.json(entry(path)) : problem(404, 'path_not_found')
    }),
    http.post('/api/v1/workspaces/:id/sites/s1/files/directories', async ({ request }) => {
      const { path } = (await request.json()) as { path: string }
      calls.push(`mkdir ${path}`)
      if (refuse[path] !== undefined) {
        return problem(403, refuse[path])
      }
      return existing.includes(path) ? problem(409, 'path_exists') : HttpResponse.json(entry(path), { status: 201 })
    }),
    http.put('/api/v1/workspaces/:id/sites/s1/files/content', ({ request }) => {
      const url = new URL(request.url)
      const path = url.searchParams.get('path') as string
      const mode = url.searchParams.get('mode') as string
      calls.push(`put ${path} ${mode}`)
      return mode === 'create' && existing.includes(path) ? problem(409, 'path_exists') : HttpResponse.json(entry(path), { status: 201 })
    }),
  )
  return calls
}

/** A file entry of the directory API of the browser. */
function fileEntry(name: string) {
  return { isFile: true, isDirectory: false, name, file: (done: (file: File) => void) => done(new File(['x'], name)) }
}

/** A directory entry of the directory API of the browser. */
function dirEntry(name: string, children: unknown[], fails = false) {
  return {
    isFile: false,
    isDirectory: true,
    name,
    createReader: () => {
      let sent = false
      return {
        readEntries: (done: (entries: unknown[]) => void, fail: (error: unknown) => void) => {
          if (fails) {
            fail(new Error('unreadable'))
            return
          }
          done(sent ? [] : children)
          sent = true
        },
      }
    },
  }
}

/** The site folder every test drops: a file, a subfolder with a file, and an empty subfolder. */
function siteFolder() {
  return dirEntry('site', [fileEntry('a.txt'), dirEntry('sub', [fileEntry('b.txt')]), dirEntry('empty', [])])
}

/** Drops entries on the explorer as the browser does. */
function drop(region: HTMLElement, entries: unknown[]): void {
  const items = entries.map((item) => ({ webkitGetAsEntry: () => item }))
  fireEvent.drop(region, { dataTransfer: { items, files: entries.map((item) => new File([], (item as { name: string }).name)), types: ['Files'] } })
}

/** Renders the explorer and returns its region. */
async function renderExplorer() {
  vi.stubGlobal('Blob', NodeBlob)
  const user = userEvent.setup()
  await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/www" onNavigate={vi.fn()} />)
  const region = (await screen.findByRole('grid')).closest('section') as HTMLElement
  return { user, region }
}

/** Confirms the dialog with a policy. */
async function confirmWith(user: ReturnType<typeof userEvent.setup>, option: string) {
  const dialog = await screen.findByRole('dialog', { name: 'Copy site' })
  await chooseOption(user, within(dialog).getByRole('combobox', { name: 'If a file exists' }), option)
  await user.click(within(dialog).getByRole('button', { name: 'Copy' }))
}

/**
 * Covers uploading folders from a drop and from the folder button: {@link useUploads} and the policy dialog of {@link Explorer}.
 *
 * Scenarios:
 * - a dropped folder asks what to do with files that exist, offering only the policies that make sense for uploads and skipping by default
 * - the folder and its subfolders are created, empty ones too, before their files are uploaded, and a folder that exists is used
 * - skip leaves files that exist, overwrite replaces them from the start, rename keeps both under a free name and stops when none is free, and stop
 *   reports the first file that exists and uploads nothing more
 * - a folder that cannot be created is reported and nothing is uploaded into it
 * - a folder that is too big, or cannot be read, is explained, and giving up at the dialog uploads nothing
 * - loose files dropped with a folder are uploaded as before
 * - the menu beside the upload button opens the folder chooser, which reads the picked folder the same way, or the file chooser
 * - a failing check for a free name is reported for the file
 */
describe('folder uploads', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  afterEach(() => vi.unstubAllGlobals())

  it('creates the folders before uploading the files, offering the policies for uploads', async () => {
    const calls = mockServer()
    const { user, region } = await renderExplorer()

    drop(region, [siteFolder()])
    const dialog = await screen.findByRole('dialog', { name: 'Copy site' })
    const select = within(dialog).getByRole('combobox', { name: 'If a file exists' })
    expect(select).toHaveTextContent('Skip it')
    expect(within(dialog).queryByRole('checkbox')).not.toBeInTheDocument()
    expect(await optionsOf(user, select)).toEqual(['Skip it', 'Keep both, renaming the copy', 'Overwrite it', 'Stop with an error'])
    await user.click(within(dialog).getByRole('button', { name: 'Copy' }))

    await waitFor(() => expect(calls).toHaveLength(5))
    expect(calls).toEqual([
      'mkdir /www/site',
      'mkdir /www/site/sub',
      'mkdir /www/site/empty',
      'put /www/site/a.txt create',
      'put /www/site/sub/b.txt create',
    ])
  })

  it('uses folders that exist and follows the policy for files that exist', async () => {
    const existing = ['/www/site', '/www/site/a.txt', '/www/site/a (1).txt']
    const calls = mockServer({ existing })
    const { user, region } = await renderExplorer()

    drop(region, [siteFolder()])
    await confirmWith(user, 'Skip it')
    await waitFor(() => expect(calls).toContain('put /www/site/sub/b.txt create'))
    expect(calls).not.toContain('put /www/site/a.txt overwrite')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    calls.length = 0
    drop(region, [siteFolder()])
    await confirmWith(user, 'Overwrite it')
    await waitFor(() => expect(calls).toContain('put /www/site/a.txt overwrite'))

    calls.length = 0
    drop(region, [siteFolder()])
    await confirmWith(user, 'Keep both, renaming the copy')
    await waitFor(() => expect(calls).toContain('put /www/site/a (2).txt create'))
  })

  it('stops at the first file that exists under the stop policy', async () => {
    const calls = mockServer({ existing: ['/www/site/a.txt'] })
    const { user, region } = await renderExplorer()

    drop(region, [siteFolder()])
    await confirmWith(user, 'Stop with an error')

    expect(await screen.findByRole('alert')).toHaveTextContent('a.txt: An item with this name already exists.')
    expect(calls).not.toContain('put /www/site/sub/b.txt create')
  })

  it('reports a file that cannot be kept under a new name', async () => {
    const calls = mockServer({ existing: ['/www/site/a.txt'] })
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files/entry', ({ request }) => HttpResponse.json(entry(new URL(request.url).searchParams.get('path') as string))))
    const { user, region } = await renderExplorer()

    drop(region, [dirEntry('site', [fileEntry('a.txt')])])
    await confirmWith(user, 'Keep both, renaming the copy')

    expect(await screen.findByRole('alert', undefined, { timeout: 15_000 })).toHaveTextContent('a.txt: An item with this name already exists.')
    expect(calls.filter((call) => call.startsWith('put'))).toEqual(['put /www/site/a.txt create'])
  }, 20_000)

  it('reports a folder that cannot be created and uploads nothing into it', async () => {
    const calls = mockServer({ refuse: { '/www/site/sub': 'remote_permission_denied' } })
    const { user, region } = await renderExplorer()

    drop(region, [siteFolder()])
    await confirmWith(user, 'Skip it')

    expect(await screen.findByRole('alert')).toHaveTextContent('site: You do not have permission to read this folder.')
    expect(calls.filter((call) => call.startsWith('put'))).toEqual([])
  })

  it('explains folders that are too big or unreadable, and does nothing when the user gives up', async () => {
    const calls = mockServer()
    const { user, region } = await renderExplorer()

    drop(region, [dirEntry('big', Array.from({ length: maxTreeEntries + 1 }, (_, index) => fileEntry(`f${index}`)))])
    expect(await screen.findByRole('alert')).toHaveTextContent('This folder is too big to copy or archive')
    await user.click(within(screen.getByRole('alert')).getByRole('button', { name: 'Dismiss' }))

    drop(region, [dirEntry('bad', [], true)])
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong. Try again.')
    await user.click(within(screen.getByRole('alert')).getByRole('button', { name: 'Dismiss' }))

    drop(region, [siteFolder()])
    await user.click(within(await screen.findByRole('dialog', { name: 'Copy site' })).getByRole('button', { name: 'Cancel' }))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(calls).toEqual([])
  })

  it('uploads the loose files dropped with a folder', async () => {
    const calls = mockServer()
    const { user, region } = await renderExplorer()

    drop(region, [siteFolder(), fileEntry('loose.txt')])
    await confirmWith(user, 'Skip it')

    await waitFor(() => expect(calls).toContain('put /www/loose.txt create'))
    await waitFor(() => expect(calls).toContain('put /www/site/sub/b.txt create'))
  })

  it('uploads the folder picked with the folder button', async () => {
    const calls = mockServer()
    const { user } = await renderExplorer()
    const input = screen.getByLabelText('Choose a folder to upload')
    const picked = (relative: string): File => {
      const file = new File(['x'], relative.slice(relative.lastIndexOf('/') + 1))
      Object.defineProperty(file, 'webkitRelativePath', { value: relative })
      return file
    }

    fireEvent.change(input, { target: { files: [picked('site/a.txt'), picked('site/sub/b.txt')] } })
    await confirmWith(user, 'Skip it')

    await waitFor(() => expect(calls).toContain('put /www/site/sub/b.txt create'))
    expect(calls.slice(0, 2)).toEqual(['mkdir /www/site', 'mkdir /www/site/sub'])
    fireEvent.change(input, { target: { files: [] } })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('opens the folder chooser from its button', async () => {
    mockServer()
    const opened: string[] = []
    vi.spyOn(HTMLInputElement.prototype, 'click').mockImplementation(function (this: HTMLInputElement) {
      opened.push(this.getAttribute('aria-label') as string)
    })
    const { user } = await renderExplorer()

    await user.click(screen.getByRole('button', { name: 'More ways to upload' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Upload folder' }))
    await user.click(screen.getByRole('button', { name: 'More ways to upload' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Upload files' }))

    expect(opened).toEqual(['Choose a folder to upload', 'Choose files to upload'])
  })

  it('reports a file when the check for a free name fails', async () => {
    mockServer({ existing: ['/www/site/a.txt'] })
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files/entry', () => new HttpResponse(null, { status: 500 })))
    const { user, region } = await renderExplorer()

    drop(region, [dirEntry('site', [fileEntry('a.txt')])])
    await confirmWith(user, 'Keep both, renaming the copy')

    expect(await screen.findByRole('alert')).toHaveTextContent('a.txt: Something went wrong. Try again.')
  })
})
