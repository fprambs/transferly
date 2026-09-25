import { Blob as NodeBlob, File as NodeFile } from 'node:buffer'

import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { TransferRecord } from '@/lib/transport'
import { problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'
import { useUiStore } from '@/state/ui-store'

import { TransfersDock } from './TransfersDock'
import { useTransfersStore } from './transfers-store'
import { clearUploadSources, recallUpload, rememberUpload } from './upload-sources'

/** An upload that stopped after 4 of 10 bytes. */
function stopped(overrides: Partial<TransferRecord> = {}): TransferRecord {
  return {
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
    ...overrides,
  }
}

/** Builds a file the mock server can receive, since the test DOM's own Blob does not work with fetch. */
function file(name: string, content: string): File {
  return new NodeFile([content], name) as unknown as File
}

/** What the mock server saw of the resumed uploads. */
interface Seen {
  /** Upload mode. */
  mode: string | null
  /** Offset of the first byte sent. */
  offset: string | null
  /** Transfer identifier. */
  transferId: string | null
  /** Bytes received, as text. */
  body: string
}

/** Answers the size of the partial file and records the uploads. */
function mockServer(): Seen[] {
  const uploads: Seen[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/:siteId/files/entry', () => HttpResponse.json({ size: 4 })),
    http.put('/api/v1/workspaces/:id/sites/:siteId/files/content', async ({ request }) => {
      const url = new URL(request.url)
      uploads.push({ mode: url.searchParams.get('mode'), offset: url.searchParams.get('offset'), transferId: url.searchParams.get('transferId'), body: await request.text() })
      return HttpResponse.json({}, { status: 201 })
    }),
  )
  return uploads
}

/** Renders the dock on the finished tab. */
async function renderDock() {
  vi.stubGlobal('Blob', NodeBlob)
  const user = userEvent.setup()
  await renderWithProviders(<TransfersDock />)
  await user.click(screen.getByRole('tab', { name: /Finished/ }))
  return user
}

/**
 * Covers resuming stopped uploads from the dock: {@link useResume} and the resume button of {@link TransferRow}.
 *
 * Scenarios:
 * - the button appears for stopped uploads this page still has the file of, or that belong to a saved site, and only then
 * - with the file at hand the upload continues at once from the size the server holds
 * - without it the user chooses the file, and the same transfer continues; choosing when nothing waits does nothing
 * - a file that is not the one being uploaded is refused with a message that can be dismissed
 * - a failure of the server shows the generic message
 */
describe('resuming uploads', () => {
  beforeEach(() => {
    clearUploadSources()
    useTransfersStore.getState().reset()
    useUiStore.setState({ queueOpen: true })
  })

  it('offers to resume only what can be resumed', async () => {
    rememberUpload('quick', { file: file('q.bin', 'x'), workspaceId: 'w1', siteId: 'q' })
    useTransfersStore.getState().upsert([
      stopped({ id: 'saved', name: 'saved.bin', status: 'failed' }),
      stopped({ id: 'quick', name: 'q.bin', siteId: undefined, status: 'canceled' }),
      stopped({ id: 'lost', name: 'lost.bin', siteId: undefined }),
      stopped({ id: 'done', name: 'done.bin', status: 'done' }),
      stopped({ id: 'down', name: 'down.bin', direction: 'download' }),
    ])

    await renderDock()

    expect(screen.getByRole('button', { name: 'Resume saved.bin' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Resume q.bin' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Resume lost.bin' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Resume done.bin' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Resume down.bin' })).not.toBeInTheDocument()
  })

  it('continues at once when the page still has the file', async () => {
    const uploads = mockServer()
    rememberUpload('t1', { file: file('big.bin', '0123456789'), workspaceId: 'w1', siteId: 's1' })
    useTransfersStore.getState().upsert([stopped()])
    const user = await renderDock()

    await user.click(screen.getByRole('button', { name: 'Resume big.bin' }))

    await waitFor(() => expect(uploads).toEqual([{ mode: 'resume', offset: '4', transferId: 't1', body: '456789' }]))
    await waitFor(() => expect(recallUpload('t1')).toBeUndefined())
  })

  it('asks for the file when the page lost it, and continues the same transfer', async () => {
    const uploads = mockServer()
    useTransfersStore.getState().upsert([stopped()])
    const user = await renderDock()
    const input = screen.getByLabelText('Choose the file to resume the upload')
    const click = vi.spyOn(input, 'click')

    await user.click(screen.getByRole('button', { name: 'Resume big.bin' }))
    expect(click).toHaveBeenCalledTimes(1)
    await user.upload(input, file('big.bin', '0123456789'))

    await waitFor(() => expect(uploads).toEqual([{ mode: 'resume', offset: '4', transferId: 't1', body: '456789' }]))
  })

  it('ignores a chosen file when no resume waits for it', async () => {
    const uploads = mockServer()
    useTransfersStore.getState().upsert([stopped()])
    const user = await renderDock()

    await user.upload(screen.getByLabelText('Choose the file to resume the upload'), file('big.bin', '0123456789'))

    expect(uploads).toEqual([])
  })

  it('refuses a file that is not the one being uploaded, and the message can be dismissed', async () => {
    const uploads = mockServer()
    useTransfersStore.getState().upsert([stopped()])
    const user = await renderDock()

    await user.click(screen.getByRole('button', { name: 'Resume big.bin' }))
    await user.upload(screen.getByLabelText('Choose the file to resume the upload'), file('other.bin', '0123456789'))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('That is not the file that was being uploaded')
    expect(uploads).toEqual([])

    await user.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the generic message when the server fails', async () => {
    server.use(http.get('/api/v1/workspaces/:id/sites/:siteId/files/entry', () => problem(502, 'remote_unavailable')))
    rememberUpload('t1', { file: file('big.bin', '0123456789'), workspaceId: 'w1', siteId: 's1' })
    useTransfersStore.getState().upsert([stopped()])
    const user = await renderDock()

    await user.click(screen.getByRole('button', { name: 'Resume big.bin' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The upload could not be resumed. Try again.')
  })

  it('clears an earlier failure when it tries again', async () => {
    let attempts = 0
    server.use(
      http.get('/api/v1/workspaces/:id/sites/:siteId/files/entry', () => (++attempts === 1 ? problem(502, 'remote_unavailable') : HttpResponse.json({ size: 4 }))),
      http.put('/api/v1/workspaces/:id/sites/:siteId/files/content', () => HttpResponse.json({}, { status: 201 })),
    )
    rememberUpload('t1', { file: file('big.bin', '0123456789'), workspaceId: 'w1', siteId: 's1' })
    useTransfersStore.getState().upsert([stopped()])
    const user = await renderDock()

    await user.click(screen.getByRole('button', { name: 'Resume big.bin' }))
    await screen.findByRole('alert')
    rememberUpload('t1', { file: file('big.bin', '0123456789'), workspaceId: 'w1', siteId: 's1' })
    await user.click(screen.getByRole('button', { name: 'Resume big.bin' }))

    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })
})
