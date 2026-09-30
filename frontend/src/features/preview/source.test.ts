import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { createApi } from '@/lib/api'
import { HttpTransport } from '@/lib/transport'
import { server } from '@/test/msw'

import { localSource, remoteSource } from './source'

/** A remote file of 10 bytes. */
const entry: RemoteEntry = { name: 'a.bin', path: '/d/a.bin', type: 'file', size: 10, mode: '-rw-r--r--', modifiedAt: '2026-09-29T12:00:00Z', modifiedPrecision: 'second' }

/**
 * Covers {@link remoteSource} and {@link localSource} (ADR-0018).
 *
 * Scenarios:
 * - a remote source reads a range of the file with a ranged request, cut to its size, and an empty range asks nothing
 * - a local source reads slices of the file, cut to its size, and knows its name, size and time
 */
describe('preview sources', () => {
  it('reads ranges of a remote file', async () => {
    const ranges: Array<string | null> = []
    server.use(
      http.get('/api/v1/workspaces/w/sites/s/files/content', ({ request }) => {
        ranges.push(request.headers.get('Range'))
        return new HttpResponse(new Uint8Array([7, 8]), { status: 206 })
      }),
    )
    const source = remoteSource(createApi(new HttpTransport()), 'w', 's', entry)

    expect(source).toMatchObject({ key: 'remote:w:s:/d/a.bin', name: 'a.bin', size: 10 })
    expect([...(await source.read(8, 64))]).toEqual([7, 8])
    expect(ranges).toEqual(['bytes=8-9'])
    expect(await source.read(20, 30)).toHaveLength(0)
    expect(ranges).toHaveLength(1)
  })

  it('reads slices of a local file', async () => {
    const file = new File(['hello world'], 'hi.txt', { lastModified: Date.UTC(2026, 8, 29) })
    const source = localSource(file, '/docs/hi.txt')

    expect(source).toMatchObject({ key: 'local:/docs/hi.txt', name: 'hi.txt', size: 11, modifiedAt: '2026-09-29T00:00:00.000Z' })
    expect(new TextDecoder().decode(await source.read(6, 100))).toBe('world')
    expect(await source.read(-5, 0)).toHaveLength(0)
  })
})
