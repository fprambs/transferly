import { Blob as NodeBlob, File as NodeFile } from 'node:buffer'

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'
import { clearUploadSources, recallUpload } from '../transfers/upload-sources'

import { Explorer } from './Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** A saved site the explorer browses. */
const prod: Site = {
  id: 's1',
  name: 'Prod',
  protocol: 'sftp',
  host: 'prod.example.com',
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

/** Builds an entry inside /www. */
function entry(name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `/www/${name}`, type, size: 2048, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'second' }
}

/** What the mock server saw of an upload. */
interface Upload {
  /** Path of the file. */
  path: string | null
  /** Upload mode. */
  mode: string | null
  /** Identifier chosen by the client. */
  transferId: string | null
  /** Announced size. */
  size: string | null
  /** Content, as text. */
  content: string
}

/** Answers listings and records the uploads; `existing` names answer with a conflict unless overwriting. */
function mockServer(existing: string[] = []): Upload[] {
  const uploads: Upload[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([entry('assets', 'directory'), entry('index.html')])),
    http.put('/api/v1/workspaces/:id/sites/s1/files/content', async ({ request }) => {
      const url = new URL(request.url)
      const path = url.searchParams.get('path')
      const mode = url.searchParams.get('mode')
      uploads.push({ path, mode, transferId: url.searchParams.get('transferId'), size: url.searchParams.get('size'), content: await request.text() })
      if (mode === 'create' && existing.some((name) => path === `/www/${name}`)) {
        return problem(409, 'path_exists')
      }
      return HttpResponse.json(entry('uploaded'), { status: 201 })
    }),
  )
  return uploads
}

/** Builds a file the mock server can receive, since the test DOM's own Blob does not work with fetch. */
function file(name: string, content: string): File {
  return new NodeFile([content], name) as unknown as File
}

/** Renders the explorer and waits for the table. */
async function renderExplorer() {
  vi.stubGlobal('Blob', NodeBlob)
  const user = userEvent.setup()
  await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/www" onNavigate={vi.fn()} />)
  const grid = await screen.findByRole('grid')
  return { user, grid }
}

/**
 * Covers uploads and downloads in the explorer: {@link useUploads}, {@link UploadConflictDialog} and the download actions.
 *
 * Scenarios:
 * - chosen files are uploaded one after another into the folder in view, with their size and a transfer identifier
 * - a file that exists asks before overwriting; overwrite sends it again, skip and closing the dialog do not
 * - the source of a failed upload is kept so it can be resumed, and the source of a finished, skipped or overwritten one is not
 * - failures are listed by file and can be dismissed, and the other files still upload
 * - dropping files uploads them, and dragging over the folder shows a hint that goes away
 * - files download from the inspector and the context menu (folders are covered by the folder download tests)
 */
describe('uploads and downloads', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [prod] })
    clearUploadSources()
  })

  it('uploads the chosen files in order', async () => {
    const uploads = mockServer()
    const { user } = await renderExplorer()

    await user.upload(screen.getByLabelText('Choose files to upload'), [file('a.txt', 'alpha'), file('b.txt', 'bravo!')])

    await waitFor(() => expect(uploads).toHaveLength(2))
    expect(uploads.map((upload) => [upload.path, upload.mode, upload.size, upload.content])).toEqual([
      ['/www/a.txt', 'create', '5', 'alpha'],
      ['/www/b.txt', 'create', '6', 'bravo!'],
    ])
    expect(uploads[0].transferId).toMatch(/^[0-9a-f-]{36}$/)
    expect(uploads[0].transferId).not.toBe(uploads[1].transferId)
  })

  it('opens the file picker from the toolbar', async () => {
    mockServer()
    const { user } = await renderExplorer()
    const input = screen.getByLabelText('Choose files to upload')
    const click = vi.spyOn(input, 'click')

    await user.click(screen.getByRole('button', { name: 'Upload' }))

    expect(click).toHaveBeenCalledTimes(1)
  })

  it('asks before overwriting and overwrites when told to', async () => {
    const uploads = mockServer(['a.txt'])
    const { user } = await renderExplorer()

    await user.upload(screen.getByLabelText('Choose files to upload'), file('a.txt', 'new'))
    const dialog = await screen.findByRole('dialog', { name: 'a.txt already exists' })
    await user.click(within(dialog).getByRole('button', { name: 'Overwrite' }))

    await waitFor(() => expect(uploads.map((upload) => upload.mode)).toEqual(['create', 'overwrite']))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('skips a file when told to, and when the dialog is closed', async () => {
    const uploads = mockServer(['a.txt', 'b.txt'])
    const { user } = await renderExplorer()

    await user.upload(screen.getByLabelText('Choose files to upload'), [file('a.txt', 'x'), file('b.txt', 'y')])
    await user.click(within(await screen.findByRole('dialog', { name: 'a.txt already exists' })).getByRole('button', { name: 'Skip' }))
    await screen.findByRole('dialog', { name: 'b.txt already exists' })
    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(uploads.map((upload) => upload.mode)).toEqual(['create', 'create'])
  })

  it('lists the files that failed, keeps uploading the rest and can be dismissed', async () => {
    const uploads: string[] = []
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([entry('index.html')])),
      http.put('/api/v1/workspaces/:id/sites/s1/files/content', ({ request }) => {
        const path = new URL(request.url).searchParams.get('path') as string
        uploads.push(path)
        return path.endsWith('bad.txt') ? problem(507, 'remote_quota_exceeded') : HttpResponse.json(entry('ok'), { status: 201 })
      }),
    )
    const { user } = await renderExplorer()

    await user.upload(screen.getByLabelText('Choose files to upload'), [file('bad.txt', 'x'), file('good.txt', 'y')])

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Some files could not be uploaded')
    expect(alert).toHaveTextContent('bad.txt')
    expect(alert).toHaveTextContent('Something went wrong. Try again.')
    expect(uploads).toEqual(['/www/bad.txt', '/www/good.txt'])

    await user.click(within(alert).getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('keeps the source of a failed upload only', async () => {
    const ids: Record<string, string> = {}
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([entry('index.html')])),
      http.put('/api/v1/workspaces/:id/sites/s1/files/content', ({ request }) => {
        const url = new URL(request.url)
        const path = url.searchParams.get('path') as string
        ids[`${path}:${url.searchParams.get('mode')}`] = url.searchParams.get('transferId') as string
        if (path.endsWith('bad.txt')) {
          return problem(502, 'remote_unavailable')
        }
        return path.endsWith('taken.txt') && url.searchParams.get('mode') === 'create' ? problem(409, 'path_exists') : HttpResponse.json(entry('ok'), { status: 201 })
      }),
    )
    const { user } = await renderExplorer()

    await user.upload(screen.getByLabelText('Choose files to upload'), [file('bad.txt', 'x'), file('good.txt', 'y'), file('taken.txt', 'z')])
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Overwrite' }))

    await waitFor(() => expect(ids['/www/taken.txt:overwrite']).toBeDefined())
    expect(recallUpload(ids['/www/bad.txt:create'])?.file.name).toBe('bad.txt')
    expect(recallUpload(ids['/www/good.txt:create'])).toBeUndefined()
    expect(recallUpload(ids['/www/taken.txt:create'])).toBeUndefined()
    expect(recallUpload(ids['/www/taken.txt:overwrite'])).toBeUndefined()
  })

  it('uploads dropped files and shows a hint while dragging', async () => {
    const uploads = mockServer()
    const { grid } = await renderExplorer()
    const region = grid.closest('section') as HTMLElement

    fireEvent.dragOver(region)
    expect(screen.getByText('Drop files to upload them here')).toBeInTheDocument()
    fireEvent.dragLeave(region)
    expect(screen.queryByText('Drop files to upload them here')).not.toBeInTheDocument()

    fireEvent.dragOver(region)
    fireEvent.drop(region, { dataTransfer: { files: [file('dropped.txt', 'drop')], types: ['Files'] } })

    await waitFor(() => expect(uploads.map((upload) => upload.path)).toEqual(['/www/dropped.txt']))
    expect(screen.queryByText('Drop files to upload them here')).not.toBeInTheDocument()
  })

  it('downloads a file from the inspector and from the context menu', async () => {
    mockServer()
    const links: string[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      links.push(this.href)
    })
    const { user, grid } = await renderExplorer()

    await user.click(within(grid).getByText('index.html'))
    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'Download' }))
    fireEvent.contextMenu(within(grid).getByText('index.html'))
    await user.click(await screen.findByRole('menuitem', { name: 'Download' }))

    expect(links).toHaveLength(2)
    for (const link of links) {
      const url = new URL(link)
      expect(url.pathname).toBe(`/api/v1/workspaces/${workspaceId}/sites/s1/files/content`)
      expect(url.searchParams.get('path')).toBe('/www/index.html')
      expect(url.searchParams.get('transferId')).toMatch(/^[0-9a-f-]{36}$/)
    }
    expect(links[0]).not.toBe(links[1])
  })
})
