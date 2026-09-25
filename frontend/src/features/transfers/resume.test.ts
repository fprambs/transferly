import { Blob as NodeBlob, File as NodeFile } from 'node:buffer'

import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createApi } from '@/lib/api'
import { HttpTransport, type TransferRecord } from '@/lib/transport'
import { problem } from '@/test/handlers'
import { server } from '@/test/msw'

import { isStoppedUpload, resumeUpload, ResumeMismatchError } from './resume'

/** A transfer that stopped after part of the file. */
const record: TransferRecord = {
  id: 't1',
  workspaceId: 'w1',
  siteId: 's1',
  siteName: 'Prod',
  direction: 'upload',
  path: '/www/big.bin',
  name: 'big.bin',
  status: 'interrupted',
  totalBytes: 10,
  bytesDone: 4,
  attempts: 1,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** What the mock server saw of an upload. */
interface Seen {
  /** Upload mode. */
  mode: string | null
  /** Offset of the first byte sent. */
  offset: string | null
  /** Transfer identifier. */
  transferId: string | null
  /** Announced size. */
  size: string | null
  /** Bytes received, as text. */
  body: string
}

/** Answers the size of the partial file, or an error, and records the upload. */
function mockServer(partial: number | 'missing' | 'broken'): Seen[] {
  const uploads: Seen[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files/entry', () => {
      if (partial === 'missing') {
        return problem(404, 'path_not_found')
      }
      return partial === 'broken' ? problem(502, 'remote_unavailable') : HttpResponse.json({ ...record, size: partial })
    }),
    http.put('/api/v1/workspaces/:id/sites/s1/files/content', async ({ request }) => {
      const url = new URL(request.url)
      uploads.push({
        mode: url.searchParams.get('mode'),
        offset: url.searchParams.get('offset'),
        transferId: url.searchParams.get('transferId'),
        size: url.searchParams.get('size'),
        body: await request.text(),
      })
      return HttpResponse.json({}, { status: 201 })
    }),
  )
  return uploads
}

/** Builds a file the mock server can receive, since the test DOM's own Blob does not work with fetch. */
function file(name: string, content: string): File {
  return new NodeFile([content], name) as unknown as File
}

/**
 * Covers {@link resumeUpload} and {@link isStoppedUpload}.
 *
 * Scenarios:
 * - the rest of the file is sent from the size the server holds, under the same transfer identifier and with the whole size
 * - a partial file that is gone starts over with the whole file
 * - a partial file larger than the file is capped at the file's size
 * - a file with another name or size is refused before anything is sent, and an unknown size is not checked
 * - other errors of the size check are raised
 * - only uploads that failed, were interrupted or canceled count as stopped
 */
describe('resumeUpload', () => {
  beforeEach(() => {
    vi.stubGlobal('Blob', NodeBlob)
  })

  const api = createApi(new HttpTransport())

  it('sends the rest of the file from the size the server holds', async () => {
    const uploads = mockServer(4)

    await resumeUpload(api, record, { file: file('big.bin', '0123456789'), siteId: 's1' })

    expect(uploads).toEqual([{ mode: 'resume', offset: '4', transferId: 't1', size: '10', body: '456789' }])
  })

  it('starts over when the partial file is gone', async () => {
    const uploads = mockServer('missing')

    await resumeUpload(api, record, { file: file('big.bin', '0123456789'), siteId: 's1' })

    expect(uploads).toEqual([{ mode: 'create', offset: null, transferId: 't1', size: '10', body: '0123456789' }])
  })

  it('starts from the beginning when nothing arrived yet', async () => {
    const uploads = mockServer(0)

    await resumeUpload(api, record, { file: file('big.bin', '0123456789'), siteId: 's1' })

    expect(uploads).toEqual([{ mode: 'resume', offset: '0', transferId: 't1', size: '10', body: '0123456789' }])
  })

  it('caps the offset at the size of the file', async () => {
    const uploads = mockServer(99)

    await resumeUpload(api, record, { file: file('big.bin', '0123456789'), siteId: 's1' })

    expect(uploads[0]).toMatchObject({ mode: 'resume', offset: '10', body: '' })
  })

  it('refuses a file with another name or size', async () => {
    const uploads = mockServer(4)

    await expect(resumeUpload(api, record, { file: file('other.bin', '0123456789'), siteId: 's1' })).rejects.toBeInstanceOf(ResumeMismatchError)
    await expect(resumeUpload(api, record, { file: file('big.bin', 'short'), siteId: 's1' })).rejects.toBeInstanceOf(ResumeMismatchError)
    expect(uploads).toEqual([])
  })

  it('does not check the size when the transfer does not know it', async () => {
    const uploads = mockServer(2)

    await resumeUpload(api, { ...record, totalBytes: undefined }, { file: file('big.bin', 'abcdef'), siteId: 's1' })

    expect(uploads[0]).toMatchObject({ offset: '2', body: 'cdef', size: '6' })
  })

  it('raises other errors of the size check', async () => {
    mockServer('broken')

    await expect(resumeUpload(api, record, { file: file('big.bin', '0123456789'), siteId: 's1' })).rejects.toMatchObject({ code: 'remote_unavailable' })
  })

  it('counts only stopped uploads', () => {
    expect(isStoppedUpload(record)).toBe(true)
    expect(isStoppedUpload({ ...record, status: 'failed' })).toBe(true)
    expect(isStoppedUpload({ ...record, status: 'canceled' })).toBe(true)
    expect(isStoppedUpload({ ...record, status: 'done' })).toBe(false)
    expect(isStoppedUpload({ ...record, status: 'running' })).toBe(false)
    expect(isStoppedUpload({ ...record, direction: 'download' })).toBe(false)
  })
})
