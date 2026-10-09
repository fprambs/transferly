import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { createApi } from '@/lib/api'
import { HttpTransport } from '@/lib/transport'
import { server } from '@/test/msw'

import { EntryTooLarge, entryName, localSource, readCapped, remoteSource, type ArchiveAccess } from './source'

/** A remote file of 10 bytes. */
const entry: RemoteEntry = { name: 'a.bin', path: '/d/a.bin', type: 'file', size: 10, mode: '-rw-r--r--', modifiedAt: '2026-09-29T12:00:00Z', modifiedPrecision: 'second' }

/**
 * Covers {@link remoteSource} and {@link localSource} (ADR-0018).
 *
 * Scenarios:
 * - a remote source reads a range of the file with a ranged request, cut to its size, and an empty range asks nothing; its media player
 *   streams from the download address
 * - a local source plays from a blob URL, released when the player closes, and opens the file on its first use only, reads slices of it cut to its size, and takes its name, size and time from the
 *   listing
 */
describe('preview sources', () => {
  it('rejects over-budget streams even when cancellation stalls or fails', async () => {
    for (const cancel of [() => new Promise<void>(() => {}), () => Promise.reject(new Error('already closed'))]) {
      const stream = new ReadableStream<Uint8Array>({ start(controller) { controller.enqueue(new Uint8Array(4)) }, cancel })
      await expect(readCapped(stream, 3)).rejects.toBeInstanceOf(EntryTooLarge)
    }
  })
  it('reads ranges of a remote file', async () => {
    const ranges: Array<string | null> = []
    server.use(
      http.get('/api/v1/workspaces/w/sites/s/files/content', ({ request }) => {
        ranges.push(request.headers.get('Range'))
        return new HttpResponse(new Uint8Array([7, 8]), { status: 206 })
      }),
    )
    const source = remoteSource(createApi(new HttpTransport()), 'w', 's', entry)

    expect(source).toMatchObject({ key: `remote:w:s:/d/a.bin:${entry.size}:${entry.modifiedAt}`, name: 'a.bin', size: 10 })
    expect([...(await source.read(8, 64))]).toEqual([7, 8])
    expect(ranges).toEqual(['bytes=8-9'])
    expect(await source.read(20, 30)).toHaveLength(0)
    expect(ranges).toHaveLength(1)
    const media = await (source.mediaUrl as NonNullable<typeof source.mediaUrl>)()
    expect(media.url).toBe('/api/v1/workspaces/w/sites/s/files/content?path=%2Fd%2Fa.bin')
    media.release()
  })

  it('reads slices of a local file, opening it once', async () => {
    let opened = 0
    const open = async () => (opened++, new File(['hello world'], 'hi.txt'))
    const source = localSource({ ...entry, name: 'hi.txt', path: '/docs/hi.txt', size: 11 }, open)

    expect(source).toMatchObject({ key: `local:0:/docs/hi.txt:11:${entry.modifiedAt}`, name: 'hi.txt', size: 11, modifiedAt: '2026-09-29T12:00:00Z' })
    expect(opened).toBe(0)
    expect(new TextDecoder().decode(await source.read(6, 100))).toBe('world')
    expect(await source.read(-5, 0)).toHaveLength(0)
    URL.createObjectURL = vi.fn(() => 'blob:local')
    URL.revokeObjectURL = vi.fn()
    const media = await (source.mediaUrl as NonNullable<typeof source.mediaUrl>)()
    expect(media.url).toBe('blob:local')
    media.release()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:local')
    expect(opened).toBe(1)
  })
})

/** A stream of the given chunks. */
function streamOf(...chunks: number[][]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      chunks.forEach((chunk) => controller.enqueue(new Uint8Array(chunk)))
      controller.close()
    },
  })
}

/**
 * Covers the archive sources (D2.5).
 *
 * Scenarios:
 * - a remote source lists its entries through the server and opens an entry by reading it whole, played from a blob URL, and
 *   downloadable by its own address; a failed read fails the opening
 * - an entry of a nested archive carries the chain of archives in its requests, and one of unknown size takes the size it read
 * - an entry over the cap is refused, declared or found while reading
 * - the name of an entry is its last part
 */
describe('archive sources', () => {
  const files = '/api/v1/workspaces/w/sites/s/files'
  const archive = (): ArchiveAccess => remoteSource(createApi(new HttpTransport()), 'w', 's', { ...entry, name: 'a.zip', path: '/a.zip' }).archive as ArchiveAccess

  it('lists and opens the entries of a remote archive', async () => {
    const queries: string[] = []
    let fail = true
    server.use(
      http.get(`${files}/archive-entries`, ({ request }) => {
        queries.push(new URL(request.url).search)
        return HttpResponse.json({ format: 'zip', entries: [], truncated: false })
      }),
      http.get(`${files}/archive-entry`, ({ request }) => {
        queries.push(new URL(request.url).search)
        if (fail) {
          fail = false
          return HttpResponse.json({ type: 'about:blank', title: 'x', status: 502, code: 'remote_unavailable' }, { status: 502 })
        }
        return new HttpResponse(new Uint8Array([1, 2, 3, 4]))
      }),
    )
    expect(await archive().list()).toMatchObject({ format: 'zip' })
    const zipped = { name: 'dir/in.zip', type: 'file', size: 4, packed: 4, modified: '2026-09-30T12:00:00Z' } as const
    await expect(archive().open(zipped)).rejects.toThrow()
    const source = await archive().open(zipped)
    expect(source).toMatchObject({ key: 'remote:w:s:/a.zip!dir/in.zip', name: 'in.zip', size: 4, modifiedAt: '2026-09-30T12:00:00Z' })
    expect([...(await source.read(1, 3))]).toEqual([2, 3])
    expect([...(await source.read(3, 9))]).toEqual([4])
    expect(queries.filter((query) => query.includes('entry='))).toHaveLength(2)
    URL.createObjectURL = vi.fn(() => 'blob:entry')
    URL.revokeObjectURL = vi.fn()
    const media = await (source.mediaUrl as NonNullable<typeof source.mediaUrl>)()
    media.release()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:entry')
    expect(archive().downloadUrl?.({ name: 'x.txt', type: 'file', size: 1, packed: 1 })).toBe(`${files}/archive-entry?path=%2Fa.zip&entry=x.txt`)

    const nested = source.archive as ArchiveAccess
    await nested.list()
    const inner = await nested.open({ name: 'note.txt', type: 'file', size: -1, packed: 2 })
    expect(inner).toMatchObject({ key: 'remote:w:s:/a.zip!dir/in.zip!note.txt', size: 4, modifiedAt: '' })
    expect([...(await inner.read(0, 4))]).toEqual([1, 2, 3, 4])
    expect(queries.at(-2)).toBe('?path=%2Fa.zip&inner=dir%2Fin.zip')
    expect(queries.at(-1)).toBe('?path=%2Fa.zip&inner=dir%2Fin.zip&entry=note.txt')
  })

  it('refuses entries over the cap', async () => {
    await expect(archive().open({ name: 'big', type: 'file', size: 65 * 1024 * 1024, packed: 1 })).rejects.toBeInstanceOf(EntryTooLarge)
    await expect(readCapped(streamOf([1, 2], [3, 4]), 3)).rejects.toBeInstanceOf(EntryTooLarge)
    expect([...(await readCapped(streamOf([1, 2], [3])))]).toEqual([1, 2, 3])
    expect(entryName({ name: 'a/b/', type: 'directory', size: 0, packed: 0 })).toBe('b')
    expect(entryName({ name: '/', type: 'directory', size: 0, packed: 0 })).toBe('/')
  })
})
