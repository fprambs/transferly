import { describe, expect, it, vi } from 'vitest'

import type { Api } from '@/lib/api/use-api'
import type { RemoteEntry } from '@/lib/api'

import { ApiError } from '@/lib/transport'
import { FakeDir, handleOf } from '@/test/fake-local'

import type { SyncRow } from './compare'
import { newEnsuredDirectories, runSyncRow } from './run-row'
import type { LocalSyncSide, RemoteSyncSide } from './sides'

/** Builds a row for a path, defaulting to a file update. */
function row(relativePath: string, action: SyncRow['action'] = 'update'): SyncRow {
  const entry: RemoteEntry = { name: relativePath.slice(relativePath.lastIndexOf('/') + 1), path: `/x/${relativePath}`, type: 'file', size: 4, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second' }
  return { relativePath, action, reason: action === 'update' ? 'time' : 'missing', source: action === 'delete' ? null : entry, target: action === 'create' ? null : entry }
}

/** A remote side of the workspace `w`, site `s`, at the given folder. */
function remote(path: string, siteId = 's'): RemoteSyncSide {
  return { kind: 'remote', workspaceId: 'w', siteId, path }
}

/** A readable stream that delivers text, standing in for a download body. */
function streamOf(text: string): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(text))
      controller.close()
    },
  })
}

/** A minimal fake of the typed API, with the calls a row run may need. */
function fakeApi(overrides: Partial<Api['files'] & { copiesCreate: Api['copies']['create']; transferGet: Api['transfers']['get'] }> = {}): Api {
  return {
    files: {
      mkdir: overrides.mkdir ?? vi.fn().mockResolvedValue(undefined),
      remove: overrides.remove ?? vi.fn().mockResolvedValue(undefined),
      download: overrides.download ?? vi.fn().mockResolvedValue(streamOf('data')),
      upload: overrides.upload ?? vi.fn().mockResolvedValue(undefined),
    },
    copies: { create: overrides.copiesCreate ?? vi.fn().mockResolvedValue(undefined) },
    transfers: { get: overrides.transferGet ?? vi.fn().mockResolvedValue({ status: 'done' }) },
  } as unknown as Api
}

/**
 * Covers {@link runSyncRow}.
 *
 * Scenarios:
 * - a create between two remote sites makes the target folder once and copies the file, waiting for the copy to actually finish and
 *   failing the row when it does not
 * - an update from a remote source to a local target downloads and overwrites the file, reusing an already made folder
 * - a create from a local source to a remote target uploads the file with the create mode
 * - a delete removes the file from a remote target, or from a local one
 * - making a folder that already exists is not an error, but another failure to make it is, and a folder not yet ensured is actually made
 * - two local folders are refused, since a dual pane never opens two of them at once
 */
describe('runSyncRow', () => {
  it('copies a file between two remote sites, making the target folder once', async () => {
    const mkdir = vi.fn().mockResolvedValue(undefined)
    const copiesCreate = vi.fn().mockResolvedValue(undefined)
    const api = fakeApi({ mkdir, copiesCreate })
    const ensured = newEnsuredDirectories()

    await runSyncRow(api, remote('/src'), remote('/dst', 'target'), row('reports/a.txt', 'create'), ensured)
    await runSyncRow(api, remote('/src'), remote('/dst', 'target'), row('reports/b.txt', 'create'), ensured)

    expect(mkdir).toHaveBeenCalledTimes(2)
    expect(mkdir).toHaveBeenNthCalledWith(1, 'w', 'target', '/dst')
    expect(mkdir).toHaveBeenNthCalledWith(2, 'w', 'target', '/dst/reports')
    expect(copiesCreate).toHaveBeenCalledTimes(2)
    expect(copiesCreate).toHaveBeenCalledWith('w', expect.objectContaining({ sourceSiteId: 's', sourcePath: '/src/reports/a.txt', targetSiteId: 'target', targetPath: '/dst/reports/a.txt', policy: 'overwrite' }))
  })

  it('waits for a copy that starts out queued to finish before moving on', async () => {
    const transferGet = vi.fn().mockResolvedValueOnce({ status: 'queued' }).mockResolvedValueOnce({ status: 'running' }).mockResolvedValue({ status: 'done' })
    const api = fakeApi({ transferGet })
    await runSyncRow(api, remote('/src'), remote('/dst'), row('a.txt', 'create'), newEnsuredDirectories())
    expect(transferGet).toHaveBeenCalledTimes(3)
  })

  it('fails a row whose copy did not finish', async () => {
    const api = fakeApi({ transferGet: vi.fn().mockResolvedValue({ status: 'failed' }) })
    await expect(runSyncRow(api, remote('/src'), remote('/dst'), row('a.txt', 'create'), newEnsuredDirectories())).rejects.toThrow('failed')
  })

  it('downloads from a remote source into a local target, reusing a folder already made', async () => {
    const download = vi.fn().mockResolvedValue(streamOf('hello'))
    const api = fakeApi({ download })
    const dest = new FakeDir('dest')
    dest.withDir('reports')
    const local: LocalSyncSide = { kind: 'local', root: handleOf(dest), path: '/' }
    const ensured = newEnsuredDirectories()
    ensured.local.add('/reports')

    await runSyncRow(api, remote('/src'), local, row('reports/a.txt', 'update'), ensured)

    expect(download).toHaveBeenCalledWith('w', 's', '/src/reports/a.txt', expect.any(String))
    const saved = (await dest.getDirectoryHandle('reports')).children.get('a.txt')
    expect(saved).toBeDefined()
  })

  it('uploads a local file to a remote target with the create mode', async () => {
    const upload = vi.fn().mockResolvedValue(undefined)
    const mkdir = vi.fn().mockResolvedValue(undefined)
    const api = fakeApi({ upload, mkdir })
    const src = new FakeDir('src').withFile('a.txt', 'hi')
    const local: LocalSyncSide = { kind: 'local', root: handleOf(src), path: '/' }
    const ensured = newEnsuredDirectories()

    await runSyncRow(api, local, remote('/dst'), row('a.txt', 'create'), ensured)

    expect(upload).toHaveBeenCalledWith('w', 's', '/dst/a.txt', expect.anything(), { mode: 'create' })

    await runSyncRow(api, local, remote('/dst'), row('a.txt', 'update'), ensured)
    expect(upload).toHaveBeenLastCalledWith('w', 's', '/dst/a.txt', expect.anything(), { mode: 'overwrite' })
  })

  it('removes the file from whichever side is the target', async () => {
    const remove = vi.fn().mockResolvedValue(undefined)
    const api = fakeApi({ remove })
    await runSyncRow(api, remote('/src'), remote('/dst'), row('a.txt', 'delete'), newEnsuredDirectories())
    expect(remove).toHaveBeenCalledWith('w', 's', '/dst/a.txt', false)

    const dest = new FakeDir('dest').withFile('a.txt', 'old')
    const local: LocalSyncSide = { kind: 'local', root: handleOf(dest), path: '/' }
    await runSyncRow(fakeApi(), remote('/src'), local, row('a.txt', 'delete'), newEnsuredDirectories())
    expect(dest.children.has('a.txt')).toBe(false)
  })

  it('does not fail when the target folder already exists', async () => {
    const mkdir = vi.fn().mockRejectedValue(new ApiError({ type: 'about:blank', title: 'x', status: 409, code: 'path_exists' }))
    const api = fakeApi({ mkdir, copiesCreate: vi.fn().mockResolvedValue(undefined) })
    await expect(runSyncRow(api, remote('/src'), remote('/dst'), row('a.txt', 'create'), newEnsuredDirectories())).resolves.toBeUndefined()
  })

  it('rethrows a folder failure that is not about the folder already existing', async () => {
    const mkdir = vi.fn().mockRejectedValue(new ApiError({ type: 'about:blank', title: 'x', status: 502, code: 'remote_unavailable' }))
    const api = fakeApi({ mkdir })
    await expect(runSyncRow(api, remote('/src'), remote('/dst'), row('a.txt', 'create'), newEnsuredDirectories())).rejects.toMatchObject({ code: 'remote_unavailable' })
  })

  it('refuses to sync between two local folders', async () => {
    const a = new FakeDir('a')
    const b = new FakeDir('b')
    const localA: LocalSyncSide = { kind: 'local', root: handleOf(a), path: '/' }
    const localB: LocalSyncSide = { kind: 'local', root: handleOf(b), path: '/' }
    await expect(runSyncRow(fakeApi(), localA, localB, row('a.txt', 'create'), newEnsuredDirectories())).rejects.toThrow('cannot sync between two local folders')
  })

  it('makes a local folder that was not already ensured', async () => {
    const dest = new FakeDir('dest')
    const local: LocalSyncSide = { kind: 'local', root: handleOf(dest), path: '/' }
    const download = vi.fn().mockResolvedValue(streamOf('hello'))
    await runSyncRow(fakeApi({ download }), remote('/src'), local, row('reports/a.txt', 'create'), newEnsuredDirectories())
    const reportsDir = await dest.getDirectoryHandle('reports')
    expect(reportsDir.children.get('a.txt')).toBeDefined()
  })
})
