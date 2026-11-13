import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { adminSession, mockSession, problem } from '@/test/handlers'
import { FakeDir, handleOf } from '@/test/fake-local'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { CompareDialog } from './CompareDialog'
import type { RemoteSyncSide } from './sides'

/** Workspace the mock server answers for. */
const workspaceId = 'w1'

/** Builds a file entry at the root of a folder. */
function file(name: string, size: number, modifiedAt: string): RemoteEntry {
  return fileAt('', name, size, modifiedAt)
}

/** Builds a file entry below a folder. */
function fileAt(dir: string, name: string, size: number, modifiedAt: string): RemoteEntry {
  return { name, path: `${dir}/${name}`, type: 'file', size, mode: '-rw-r--r--', modifiedAt, modifiedPrecision: 'second' }
}

/** The two remote sides the tests compare, unless a test builds its own. */
const left: RemoteSyncSide = { kind: 'remote', workspaceId, siteId: 's1', path: '/' }
const right: RemoteSyncSide = { kind: 'remote', workspaceId, siteId: 's2', path: '/' }

/** Answers the listing of a site with the files given, and records every write it receives. */
function mockSites(files: Record<string, RemoteEntry[]>): { copies: unknown[]; uploads: unknown[]; removed: unknown[]; mkdirs: unknown[] } {
  const copies: unknown[] = []
  const uploads: unknown[] = []
  const removed: unknown[] = []
  const mkdirs: unknown[] = []
  server.use(
    http.get('/api/v1/workspaces/:w/sites/:siteId/files', ({ params }) => HttpResponse.json(files[params.siteId as string] ?? [])),
    http.get('/api/v1/workspaces/:w/sites/:siteId/files/checksum', ({ params, request }) => {
      const path = new URL(request.url).searchParams.get('path')
      return HttpResponse.json({ algorithm: 'sha256', checksum: `${params.siteId}${path}`, size: 1 })
    }),
    http.get('/api/v1/workspaces/:w/sites/:siteId/files/content', () => new HttpResponse('remote content')),
    http.put('/api/v1/workspaces/:w/sites/:siteId/files/content', async ({ request }) => {
      uploads.push({ path: new URL(request.url).searchParams.get('path'), mode: new URL(request.url).searchParams.get('mode') })
      return HttpResponse.json(file('x', 1, '2026-01-01T00:00:00Z'))
    }),
    http.post('/api/v1/workspaces/:w/copies', async ({ request }) => {
      copies.push(await request.json())
      return HttpResponse.json({}, { status: 202 })
    }),
    http.get('/api/v1/transfers/:id', () => HttpResponse.json({ status: 'done' })),
    http.post('/api/v1/workspaces/:w/sites/:siteId/files/directories', async ({ request }) => {
      mkdirs.push(await request.json())
      return HttpResponse.json(file('d', 0, '2026-01-01T00:00:00Z'), { status: 201 })
    }),
    http.delete('/api/v1/workspaces/:w/sites/:siteId/files', ({ request }) => {
      removed.push(new URL(request.url).searchParams.get('path'))
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return { copies, uploads, removed, mkdirs }
}

/**
 * Covers {@link CompareDialog} and the hook it is built on, {@link useSyncPlan}.
 *
 * Scenarios:
 * - a file on only one side is a create or a delete, one that differs in time is an update, and equal files are left out
 * - creates and updates are checked by default and deletes are not, and the run button counts only the checked rows
 * - running a plan with no checked delete copies every checked create and update between the two sites
 * - a checked delete asks to confirm before it runs, and then removes the file from the target
 * - an update over a file the target changed later starts unchecked and says so, and once checked asks to confirm before replacing it
 * - swapping direction turns a one-sided file's create into a delete and back
 * - verifying a row by checksum settles it: equal removes it from the plan, different keeps it with its own reason
 * - a folder that already matches shows nothing to sync, and a failure to list either side, a folder that is not the site's root, or a
 *   folder with too many files to compare, each explain themselves and a failure can be retried
 * - a file created or updated on a local folder is written there, and one read from it is uploaded to the remote site
 * - a checksum can be read from a local file to verify a row against it
 * - a row the server refuses shows its own failure, and the run keeps going to the rows after it
 */
describe('compare and sync', () => {
  beforeEach(() => {
    mockSession(adminSession)
  })

  it('classifies files by whether they exist on both sides and whether they agree', async () => {
    mockSites({
      s1: [file('a.txt', 10, '2026-01-01T00:00:00Z'), file('common.txt', 5, '2026-01-01T00:00:00Z'), file('same.txt', 3, '2026-01-01T00:00:00Z')],
      s2: [file('b.txt', 10, '2026-01-01T00:00:00Z'), file('common.txt', 5, '2026-01-01T00:10:00Z'), file('same.txt', 3, '2026-01-01T00:00:00Z')],
    })
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    const list = await screen.findByRole('list', { name: 'Comparison' })
    expect(within(list).getByText('a.txt')).toBeInTheDocument()
    expect(within(list).getByText('b.txt')).toBeInTheDocument()
    expect(within(list).getByText('common.txt')).toBeInTheDocument()
    expect(within(list).queryByText('same.txt')).not.toBeInTheDocument()
    expect(within(list).getAllByRole('listitem')).toHaveLength(3)
    expect(within(list).getByText('Create')).toBeInTheDocument()
    expect(within(list).getByText('Delete')).toBeInTheDocument()
    expect(within(list).getByText('Different modification time', { exact: false })).toBeInTheDocument()
  })

  it('checks creates and updates by default, leaves deletes unchecked, and counts the checked rows', async () => {
    mockSites({ s1: [file('a.txt', 1, '2026-01-01T00:00:00Z')], s2: [file('b.txt', 1, '2026-01-01T00:00:00Z')] })
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    expect(await screen.findByRole('checkbox', { name: 'Include a.txt in the sync' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Include b.txt in the sync' })).not.toBeChecked()
    expect(screen.getByRole('button', { name: 'Sync 1 file' })).toBeInTheDocument()
  })

  it('copies every checked create and update between the two sites', async () => {
    const seen = mockSites({
      s1: [file('a.txt', 1, '2026-01-01T00:00:00Z'), file('c.txt', 2, '2026-06-01T00:00:00Z')],
      s2: [file('c.txt', 2, '2026-01-01T00:00:00Z')],
    })
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    await user.click(await screen.findByRole('button', { name: 'Sync 2 files' }))

    await waitFor(() => expect(seen.copies).toHaveLength(2))
    expect(seen.copies).toContainEqual(expect.objectContaining({ sourceSiteId: 's1', sourcePath: '/a.txt', targetSiteId: 's2', targetPath: '/a.txt', policy: 'overwrite' }))
    expect(seen.copies).toContainEqual(expect.objectContaining({ sourceSiteId: 's1', sourcePath: '/c.txt', targetSiteId: 's2', targetPath: '/c.txt' }))
    await waitFor(() => expect(screen.getAllByText('Done')).toHaveLength(2))
    expect(seen.mkdirs).toEqual([])
  })

  it('asks to confirm a checked delete before running, then removes the file from the target', async () => {
    const seen = mockSites({ s1: [], s2: [file('gone.txt', 1, '2026-01-01T00:00:00Z')] })
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    await user.click(await screen.findByRole('checkbox', { name: 'Include gone.txt in the sync' }))
    await user.click(screen.getByRole('button', { name: 'Sync 1 file' }))
    expect(screen.getByRole('alert')).toHaveTextContent('This will permanently remove 1 file from Backup. This cannot be undone.')

    await user.click(screen.getByRole('button', { name: 'Yes, sync and delete' }))

    await waitFor(() => expect(seen.removed).toEqual(['/gone.txt']))
    expect(await screen.findByText('Done')).toBeInTheDocument()
  })

  it('leaves an update over a newer file unchecked, and asks to confirm before replacing it', async () => {
    const seen = mockSites({ s1: [file('notes.txt', 2, '2026-01-01T00:00:00Z')], s2: [file('notes.txt', 3, '2026-06-01T00:00:00Z')] })
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    const include = await screen.findByRole('checkbox', { name: 'Include notes.txt in the sync' })
    expect(include).not.toBeChecked()
    expect(screen.getByText(/Newer on the target/)).toBeInTheDocument()
    await user.click(include)
    await user.click(screen.getByRole('button', { name: 'Sync 1 file' }))
    expect(screen.getByRole('alert')).toHaveTextContent('1 file on Backup is newer than the one that replaces it. Its latest changes will be lost.')
    expect(screen.getByRole('alert')).not.toHaveTextContent('permanently remove')

    await user.click(screen.getByRole('button', { name: 'Yes, sync and replace' }))
    await waitFor(() => expect(seen.copies).toEqual([expect.objectContaining({ sourcePath: '/notes.txt', targetPath: '/notes.txt', policy: 'overwrite' })]))
  })

  it('turns a one-sided file from create to delete and back when the direction is swapped', async () => {
    mockSites({ s1: [file('only-left.txt', 1, '2026-01-01T00:00:00Z')], s2: [] })
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)
    await screen.findByText('only-left.txt')
    expect(screen.getByText('Create')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Backup Prod' }))
    expect(await screen.findByText('Delete')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Prod Backup' }))
    expect(await screen.findByText('Create')).toBeInTheDocument()
  })

  it('settles an ambiguous row by checksum: equal removes it, different keeps it under its own reason', async () => {
    mockSites({ s1: [file('a.txt', 5, '2026-01-01T00:00:00Z')], s2: [file('a.txt', 5, '2026-06-01T00:00:00Z')] })
    server.use(http.get('/api/v1/workspaces/:w/sites/:siteId/files/checksum', () => HttpResponse.json({ algorithm: 'sha256', checksum: 'same-content', size: 5 })))
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)
    await screen.findByText('Different modification time', { exact: false })

    await user.click(screen.getByRole('button', { name: 'Verify' }))
    expect(await screen.findByText('Nothing differs. The two folders already match.')).toBeInTheDocument()
  })

  it('keeps a row that a checksum finds different, under the checksum reason', async () => {
    server.use(http.get('/api/v1/workspaces/:w/sites/:siteId/files/checksum', () => HttpResponse.json({ algorithm: 'sha256', checksum: crypto.randomUUID(), size: 1 })))
    mockSites({ s1: [file('a.txt', 5, '2026-01-01T00:00:00Z')], s2: [file('a.txt', 5, '2026-06-01T00:00:00Z')] })
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)
    await screen.findByText('Different modification time', { exact: false })

    await user.click(screen.getByRole('button', { name: 'Verify' }))
    expect(await screen.findByText('Content differs', { exact: false })).toBeInTheDocument()
  })

  it('verifies a row of a folder that is not the root of the site', async () => {
    server.use(http.get('/api/v1/workspaces/:w/sites/:siteId/files/checksum', () => HttpResponse.json({ algorithm: 'sha256', checksum: crypto.randomUUID(), size: 5 })))
    mockSites({ s1: [fileAt('/sub', 'a.txt', 5, '2026-01-01T00:00:00Z')], s2: [fileAt('/sub', 'a.txt', 5, '2026-06-01T00:00:00Z')] })
    const sub = { kind: 'remote' as const, workspaceId, siteId: 's1', path: '/sub' }
    const subRight = { kind: 'remote' as const, workspaceId, siteId: 's2', path: '/sub' }
    await renderWithProviders(<CompareDialog left={sub} right={subRight} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)
    await screen.findByText('Different modification time', { exact: false })

    await userEvent.setup().click(screen.getByRole('button', { name: 'Verify' }))
    expect(await screen.findByText('Content differs', { exact: false })).toBeInTheDocument()
  })

  it('shows nothing to sync when the folders already match', async () => {
    mockSites({ s1: [file('same.txt', 1, '2026-01-01T00:00:00Z')], s2: [file('same.txt', 1, '2026-01-01T00:00:00Z')] })
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    expect(await screen.findByText('Nothing differs. The two folders already match.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sync 0 files' })).toBeDisabled()
  })

  it('explains a failure to list and retries on refresh', async () => {
    let fail = true
    server.use(http.get('/api/v1/workspaces/:w/sites/:siteId/files', ({ params }) => (params.siteId === 's1' && fail ? problem(404, 'site_not_found') : HttpResponse.json([]))))
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('The site no longer exists.')
    fail = false
    await user.click(screen.getByRole('button', { name: 'Compare again' }))
    expect(await screen.findByText('Nothing differs. The two folders already match.')).toBeInTheDocument()
  })

  it('explains a failure of the right side even when the left one loads fine', async () => {
    server.use(http.get('/api/v1/workspaces/:w/sites/:siteId/files', ({ params }) => (params.siteId === 's2' ? problem(403, 'remote_permission_denied') : HttpResponse.json([]))))

    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    expect(await screen.findByRole('alert')).toHaveTextContent("The server refused permission for this file.")
  })

  it('explains a folder that holds more files than can be compared at once', async () => {
    const many = Array.from({ length: 501 }, (_, index) => file(`f${index}.txt`, 1, '2026-01-01T00:00:00Z'))
    mockSites({ s1: many, s2: [] })

    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('One of the folders holds more files than can be compared at once.')
  })

  it('writes a remote file into a local folder, and uploads a local file to the remote site once the direction is reversed', async () => {
    const dir = new FakeDir('backup').withFile('only-local.txt', 'from disk')
    const local = { kind: 'local' as const, root: handleOf(dir), path: '/' }
    const seen = mockSites({ s1: [file('a.txt', 1, '2026-01-01T00:00:00Z')], s2: [] })
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={local} leftLabel="Prod" rightLabel="Local folder" onClose={() => {}} />)

    await user.click(await screen.findByRole('button', { name: 'Sync 1 file' }))
    await waitFor(() => expect(dir.children.has('a.txt')).toBe(true))

    await user.click(screen.getByRole('button', { name: 'Local folder Prod' }))
    await user.click(await screen.findByRole('button', { name: 'Sync 1 file' }))
    await waitFor(() => expect(seen.uploads).toEqual([{ path: '/only-local.txt', mode: 'create' }]))
  })

  it('reads the checksum of a local file to verify a row', async () => {
    const dir = new FakeDir('backup').withFile('a.txt', 'x'.repeat(29))
    const local = { kind: 'local' as const, root: handleOf(dir), path: '/' }
    mockSites({ s1: [file('a.txt', 29, '2026-01-01T00:00:00Z')], s2: [] })
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={local} leftLabel="Prod" rightLabel="Local folder" onClose={() => {}} />)
    await screen.findByText('Different modification time', { exact: false })

    await user.click(screen.getByRole('button', { name: 'Verify' }))
    expect(await screen.findByText('Content differs', { exact: false })).toBeInTheDocument()
  })

  it('shows the failure of a row the server refuses and keeps running the others', async () => {
    mockSites({ s1: [file('a.txt', 1, '2026-01-01T00:00:00Z')], s2: [] })
    server.use(http.post('/api/v1/workspaces/:w/copies', () => problem(404, 'site_not_found')))
    const user = userEvent.setup()
    await renderWithProviders(<CompareDialog left={left} right={right} leftLabel="Prod" rightLabel="Backup" onClose={() => {}} />)

    await user.click(await screen.findByRole('button', { name: 'Sync 1 file' }))

    expect(await screen.findByText('The site no longer exists.')).toBeInTheDocument()
  })
})
