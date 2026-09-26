import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { createApi, type RemoteEntry } from '@/lib/api'
import { HttpTransport } from '@/lib/transport'
import { FakeDir, handleOf } from '@/test/fake-local'
import { problem } from '@/test/handlers'
import { server } from '@/test/msw'

import { downloadFolderToLocal, type LocalPolicy } from './download-tree'
import { maxTreeEntries } from './tree'

/** Builds an entry of a remote folder. */
function entry(folder: string, name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `${folder === '/' ? '' : folder}/${name}`, type, size: 1, mode: '', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'minute' }
}

/** The remote tree the tests download. */
const remote: Record<string, RemoteEntry[]> = {
  '/src': [entry('/src', 'a.txt'), entry('/src', 'sub', 'directory'), entry('/src', 'link', 'symlink')],
  '/src/sub': [entry('/src/sub', 'b.txt'), entry('/src/sub', 'empty', 'directory')],
  '/src/sub/empty': [],
}

/** Answers the listings and downloads of the remote tree; a download of a path in `broken` fails. */
function mockRemote(listings: Record<string, RemoteEntry[]> = remote, broken: string[] = []): void {
  server.use(
    http.get('/api/v1/workspaces/w/sites/s/files', ({ request }) => {
      const found = listings[new URL(request.url).searchParams.get('path') as string]
      return found === undefined ? problem(404, 'path_not_found') : HttpResponse.json(found)
    }),
    http.get('/api/v1/workspaces/w/sites/s/files/content', ({ request }) => {
      const path = new URL(request.url).searchParams.get('path') as string
      return broken.includes(path) ? problem(404, 'path_not_found') : new HttpResponse(`content of ${path}`)
    }),
  )
}

/** Downloads `/src` into the root of a fake folder with a policy. */
function download(root: FakeDir, policy: LocalPolicy, destination = '/', source = '/src') {
  return downloadFolderToLocal({ api: createApi(new HttpTransport()), workspaceId: 'w', siteId: 's', source, root: handleOf(root), destination, policy })
}

/** Reads a file of a fake folder as text. */
function text(dir: FakeDir | undefined, name: string): string | undefined {
  const file = dir?.children.get(name) as { content: Uint8Array } | undefined
  return file === undefined ? undefined : new TextDecoder().decode(file.content)
}

/**
 * Covers {@link downloadFolderToLocal}.
 *
 * Scenarios:
 * - a folder is saved whole below the destination, with its subfolders and empty folders, and links are left out
 * - folders that exist are merged into; files that exist are skipped, overwritten or kept under a new name, as the policy says
 * - the fail policy stops at the first file that exists and reports it
 * - a file where a folder is needed, a download the server refuses and a disk that fails are reported per file while the rest is saved
 * - a folder that cannot be listed, a destination that is a file and a tree with too many entries are reported for the folder
 */
describe('downloadFolderToLocal', () => {
  beforeEach(() => mockRemote())

  it('saves the whole tree', async () => {
    const root = new FakeDir('root')

    const failures = await download(root, 'skip')

    expect(failures).toEqual([])
    const src = root.children.get('src') as FakeDir
    expect(text(src, 'a.txt')).toBe('content of /src/a.txt')
    expect(text(src.children.get('sub') as FakeDir, 'b.txt')).toBe('content of /src/sub/b.txt')
    expect((src.children.get('sub') as FakeDir).children.has('empty')).toBe(true)
    expect(src.children.has('link')).toBe(false)
  })

  it('follows the policy for files that exist', async () => {
    const seed = (): FakeDir => {
      const root = new FakeDir('root')
      root.withDir('src').withFile('a.txt', 'mine')
      return root
    }

    const skipped = seed()
    expect(await download(skipped, 'skip')).toEqual([])
    expect(text(skipped.children.get('src') as FakeDir, 'a.txt')).toBe('mine')
    expect(text((skipped.children.get('src') as FakeDir).children.get('sub') as FakeDir, 'b.txt')).toBe('content of /src/sub/b.txt')

    const overwritten = seed()
    expect(await download(overwritten, 'overwrite')).toEqual([])
    expect(text(overwritten.children.get('src') as FakeDir, 'a.txt')).toBe('content of /src/a.txt')

    const renamed = seed()
    expect(await download(renamed, 'rename')).toEqual([])
    const src = renamed.children.get('src') as FakeDir
    expect(text(src, 'a.txt')).toBe('mine')
    expect(text(src, 'a (1).txt')).toBe('content of /src/a.txt')
  })

  it('stops at the first file that exists under the fail policy', async () => {
    const root = new FakeDir('root')
    root.withDir('src').withFile('a.txt', 'mine')

    expect(await download(root, 'fail')).toEqual([{ name: 'a.txt', key: 'path_exists' }])
    expect((root.children.get('src') as FakeDir).children.has('sub')).toBe(false)

    const inner = new FakeDir('root')
    inner.withDir('src').withDir('sub').withFile('b.txt', 'mine')
    expect(await download(inner, 'fail')).toEqual([{ name: 'sub/b.txt', key: 'path_exists' }])
  })

  it('reports what goes wrong per file and keeps saving the rest', async () => {
    const blocked = new FakeDir('root')
    blocked.withDir('src').withFile('sub', 'a file where a folder goes')
    const failures = await download(blocked, 'skip')
    expect(failures).toEqual([{ name: 'sub', key: 'path_exists' }])
    expect(text(blocked.children.get('src') as FakeDir, 'a.txt')).toBe('content of /src/a.txt')

    mockRemote(remote, ['/src/a.txt'])
    const refused = new FakeDir('root')
    expect(await download(refused, 'skip')).toEqual([{ name: 'a.txt', key: 'path_not_found' }])
    expect(text((refused.children.get('src') as FakeDir).children.get('sub') as FakeDir, 'b.txt')).toBe('content of /src/sub/b.txt')
    expect(await download(new FakeDir('root'), 'fail')).toEqual([{ name: 'a.txt', key: 'path_not_found' }])

    mockRemote()
    const full = new FakeDir('root')
    full.withDir('src').failWrites = true
    expect(await download(full, 'skip')).toEqual([{ name: 'a.txt', key: 'localFailure' }])
  })

  it('reports a folder that cannot be listed, a destination that is a file and a tree that is too big', async () => {
    expect(await download(new FakeDir('root'), 'skip', '/', '/missing')).toEqual([{ name: 'missing', key: 'path_not_found' }])

    const clash = new FakeDir('root').withFile('src')
    expect(await download(clash, 'skip')).toEqual([{ name: 'src', key: 'path_exists' }])

    mockRemote({ '/src': Array.from({ length: maxTreeEntries + 1 }, (_, index) => entry('/src', `f${index}`)) })
    expect(await download(new FakeDir('root'), 'skip')).toEqual([{ name: 'src', key: 'tree_too_large' }])

    mockRemote({
      '/src': [entry('/src', 'sub', 'directory'), ...Array.from({ length: maxTreeEntries / 2 - 1 }, (_, index) => entry('/src', `f${index}`))],
      '/src/sub': Array.from({ length: maxTreeEntries / 2 + 1 }, (_, index) => entry('/src/sub', `g${index}`)),
    })
    expect(await download(new FakeDir('root'), 'skip')).toEqual([{ name: 'src', key: 'tree_too_large' }])
  })
})
