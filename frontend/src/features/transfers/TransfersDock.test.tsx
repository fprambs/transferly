import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { TransferRecord } from '@/lib/transport'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose } from '@/test/select'
import { renderWithProviders } from '@/test/render'
import { useUiStore } from '@/state/ui-store'

import { settleDelayMs, TransfersDock } from './TransfersDock'
import { useTransfersStore } from './transfers-store'

/** Builds a transfer record, newest first by the number in its identifier. */
function record(id: string, overrides: Partial<TransferRecord> = {}): TransferRecord {
  return {
    id,
    workspaceId: 'w1',
    siteName: 'Prod',
    direction: 'upload',
    path: `/${id}`,
    name: `${id}.bin`,
    status: 'running',
    totalBytes: 2 * 1024 ** 2,
    bytesDone: 1024 ** 2,
    attempts: 1,
    createdAt: `2026-01-01T00:00:${id.padStart(2, '0')}Z`,
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

/**
 * Covers {@link TransfersDock} and {@link TransferRow}.
 *
 * Scenarios:
 * - with no transfers it says so, and the counts and summary appear as transfers arrive
 * - an active upload shows its progress, speed and time left; a queued download and one of unknown size show what is known
 * - the finished tab lists done, failed and canceled transfers with their status and error code
 * - each tab says when it is empty
 * - cancelling calls the server, and removing calls it and forgets the transfer; failures are ignored
 * - a transfer that just ended stays on the active tab with its result for a while and then leaves it, while older ones only show under finished
 * - a copy between sites shows its own icon and the two sites
 * - a queued transfer says it waits for a slot, offers a priority and a pause, and a running one offers neither
 * - pausing, continuing and changing the priority call the server, and the row follows the state event that comes back; failures are ignored
 * - a paused transfer says so and offers to continue
 * - the activity tab lists what the user did with its time, kind, path, site and outcome, says when there is none or it cannot be loaded, keeps
 *   itself up to date while it is on show, and follows the interface language
 * - the tabs switch both ways and the choice is kept in the interface state
 * - the panel collapses and the texts follow the interface language
 * - a transfer that starts opens a collapsed queue, which collapses again a while after the last one ends; a queue the user opened stays
 */
describe('TransfersDock', () => {
  beforeEach(() => {
    useTransfersStore.getState().reset()
    useUiStore.setState({ queueOpen: true, queueTab: 'active', queueAuto: false })
    mockSession(adminSession)
    mockWorkspaceData()
  })

  it('opens by itself for a transfer and collapses again once it ends', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    useUiStore.setState({ queueOpen: false, queueAuto: false })
    await renderWithProviders(<TransfersDock />)

    act(() => useTransfersStore.getState().upsert([record('1')]))
    expect(useUiStore.getState()).toMatchObject({ queueOpen: true, queueAuto: true })
    act(() => useTransfersStore.getState().upsert([record('1', { status: 'done', bytesDone: 2 * 1024 ** 2 })]))
    act(() => vi.advanceTimersByTime(settleDelayMs - 100))
    expect(useUiStore.getState().queueOpen).toBe(true)
    act(() => vi.advanceTimersByTime(200))
    expect(useUiStore.getState().queueOpen).toBe(false)
    vi.useRealTimers()
  })

  it('stays open after the transfers when the user opened it', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderWithProviders(<TransfersDock />)

    act(() => useTransfersStore.getState().upsert([record('1')]))
    act(() => useTransfersStore.getState().upsert([record('1', { status: 'done', bytesDone: 2 * 1024 ** 2 })]))
    act(() => vi.advanceTimersByTime(settleDelayMs * 2))
    expect(useUiStore.getState()).toMatchObject({ queueOpen: true, queueAuto: false })
    vi.useRealTimers()
  })

  it('says when there are no transfers', async () => {
    await renderWithProviders(<TransfersDock />)

    expect(screen.getByText('Nothing is transferring.')).toBeInTheDocument()
  })

  it('shows progress, speed and time left of a running upload', async () => {
    useTransfersStore.getState().upsert([record('1', { name: 'backup.tar' })])
    useTransfersStore.getState().progress('1', 1024 ** 2, 2 * 1024 ** 2, 1000)
    useTransfersStore.getState().progress('1', 1024 ** 2 + 512 * 1024, 2 * 1024 ** 2, 2000)
    await renderWithProviders(<TransfersDock />)

    const bar = screen.getByRole('progressbar', { name: 'Progress of backup.tar' })
    expect(bar).toHaveAttribute('aria-valuenow', '75')
    const row = screen.getByRole('listitem')
    expect(row).toHaveTextContent('backup.tar')
    expect(row).toHaveTextContent('Prod')
    expect(row).toHaveTextContent('1.5 MB of 2.0 MB')
    expect(row).toHaveTextContent('512.0 KB/s')
    expect(row).toHaveTextContent('1s left')
    expect(screen.getByText('1 active')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Active (1)' })).toBeInTheDocument()
    expect(screen.getByLabelText('Upload')).toBeInTheDocument()
  })

  it('shows what is known of a queued download and of a transfer of unknown size', async () => {
    useTransfersStore.getState().upsert([
      record('1', { direction: 'download', status: 'queued', bytesDone: 0 }),
      record('2', { totalBytes: undefined, bytesDone: 2048 }),
    ])
    await renderWithProviders(<TransfersDock />)

    const rows = screen.getAllByRole('listitem')
    expect(rows).toHaveLength(2)
    expect(within(rows[1]).getByLabelText('Download')).toBeInTheDocument()
    expect(rows[1]).toHaveTextContent('0 B of 2.0 MB')
    expect(rows[0]).toHaveTextContent('2.0 KB')
    expect(rows[0]).not.toHaveTextContent(' of ')
    expect(within(rows[0]).getByRole('progressbar')).not.toHaveAttribute('aria-valuenow')
  })

  it('lists finished transfers with their status', async () => {
    useTransfersStore.getState().upsert([
      record('1', { status: 'done', bytesDone: 2 * 1024 ** 2 }),
      record('2', { status: 'failed', errorCode: 'remote_unavailable' }),
      record('3', { status: 'canceled' }),
    ])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    expect(screen.getByText('Nothing is transferring.')).toBeInTheDocument()
    await user.click(screen.getByRole('tab', { name: 'Finished (3)' }))

    const rows = screen.getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('Canceled')
    expect(rows[1]).toHaveTextContent('Failed · remote_unavailable')
    expect(rows[2]).toHaveTextContent('Done')
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })

  it('says when there are no finished transfers', async () => {
    useTransfersStore.getState().upsert([record('1')])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    await user.click(screen.getByRole('tab', { name: 'Finished (0)' }))

    expect(screen.getByText('No finished transfers.')).toBeInTheDocument()
  })

  it('cancels an active transfer on the server, and ignores a failure', async () => {
    const cancelled: string[] = []
    server.use(
      http.post('/api/v1/transfers/1/cancel', () => (cancelled.push('1'), new HttpResponse(null, { status: 204 }))),
      http.post('/api/v1/transfers/2/cancel', () => problem(409, 'transfer_not_active')),
    )
    useTransfersStore.getState().upsert([record('1', { name: 'a.bin' }), record('2', { name: 'b.bin' })])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    await user.click(screen.getByRole('button', { name: 'Cancel a.bin' }))
    await user.click(screen.getByRole('button', { name: 'Cancel b.bin' }))

    await waitFor(() => expect(cancelled).toEqual(['1']))
    expect(screen.getByRole('button', { name: 'Cancel b.bin' })).toBeInTheDocument()
  })

  it('removes a finished transfer, and keeps it when the server refuses', async () => {
    server.use(
      http.delete('/api/v1/transfers/1', () => new HttpResponse(null, { status: 204 })),
      http.delete('/api/v1/transfers/2', () => problem(409, 'transfer_active')),
    )
    useTransfersStore.getState().upsert([record('1', { status: 'done', name: 'a.bin' }), record('2', { status: 'failed', name: 'b.bin' })])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)
    await user.click(screen.getByRole('tab', { name: 'Finished (2)' }))

    await user.click(screen.getByRole('button', { name: 'Remove a.bin from the list' }))
    await waitFor(() => expect(useTransfersStore.getState().items['1']).toBeUndefined())

    await user.click(screen.getByRole('button', { name: 'Remove b.bin from the list' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remove b.bin from the list' })).toBeInTheDocument())
    expect(useTransfersStore.getState().items['2']).toBeDefined()
  })

  it('keeps a transfer that just ended on the active tab for a while', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const ended = new Date(Date.now() - 2000).toISOString()
    const old = new Date(Date.now() - 60_000).toISOString()
    useTransfersStore.getState().upsert([
      record('1', { name: 'new.bin', status: 'done', finishedAt: ended }),
      record('2', { name: 'old.bin', status: 'done', finishedAt: old }),
    ])
    await renderWithProviders(<TransfersDock />)

    const rows = screen.getAllByRole('listitem')
    expect(rows).toHaveLength(1)
    expect(rows[0]).toHaveTextContent('new.bin')
    expect(rows[0]).toHaveTextContent('Done')
    expect(screen.getByRole('tab', { name: 'Active (0)' })).toBeInTheDocument()

    await act(() => vi.advanceTimersByTimeAsync(20_000))
    expect(screen.queryByRole('listitem')).not.toBeInTheDocument()
    expect(screen.getByText('Nothing is transferring.')).toBeInTheDocument()
    vi.useRealTimers()
  })

  it('shows a copy between sites with its own icon', async () => {
    useTransfersStore.getState().upsert([record('1', { direction: 'copy', siteName: 'Prod → Backup' })])
    await renderWithProviders(<TransfersDock />)

    expect(screen.getByLabelText('Copy')).toBeInTheDocument()
    expect(screen.getByRole('listitem')).toHaveTextContent('Prod → Backup')
  })

  it('offers to prioritize and pause only the transfers that wait', async () => {
    useTransfersStore.getState().upsert([record('1', { name: 'run.bin' }), record('2', { name: 'wait.bin', status: 'queued', bytesDone: 0, priority: 1 })])
    await renderWithProviders(<TransfersDock />)

    const [waiting, running] = screen.getAllByRole('listitem')
    expect(waiting).toHaveTextContent('Waiting for a free slot')
    expect(within(waiting).getByRole('combobox', { name: 'Priority of wait.bin' })).toHaveTextContent('High')
    expect(within(waiting).getByRole('button', { name: 'Pause wait.bin' })).toBeInTheDocument()
    expect(within(running).queryByRole('combobox')).not.toBeInTheDocument()
    expect(within(running).queryByRole('button', { name: /Pause/ })).not.toBeInTheDocument()
    expect(within(running).getByRole('button', { name: 'Cancel run.bin' })).toBeInTheDocument()
  })

  it('pauses and continues a waiting transfer, and ignores a failure', async () => {
    const queued = record('1', { name: 'a.bin', status: 'queued', bytesDone: 0 })
    const calls: string[] = []
    server.use(
      http.post('/api/v1/transfers/1/pause', () => (calls.push('pause'), HttpResponse.json({ ...queued, paused: true, priority: 0 }))),
      http.post('/api/v1/transfers/1/resume', () => (calls.push('resume'), HttpResponse.json({ ...queued, paused: false, priority: 0 }))),
      http.post('/api/v1/transfers/2/pause', () => problem(409, 'transfer_not_queued')),
    )
    useTransfersStore.getState().upsert([queued, record('2', { name: 'b.bin', status: 'queued', bytesDone: 0 })])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    await user.click(screen.getByRole('button', { name: 'Pause b.bin' }))
    await user.click(screen.getByRole('button', { name: 'Pause a.bin' }))
    await waitFor(() => expect(calls).toEqual(['pause']))
    act(() => useTransfersStore.getState().upsert([{ ...queued, paused: true }]))
    const resume = await screen.findByRole('button', { name: 'Let a.bin start' })
    expect(screen.getAllByRole('listitem')[1]).toHaveTextContent('Paused')
    expect(screen.getByRole('button', { name: 'Pause b.bin' })).toBeInTheDocument()

    await user.click(resume)
    await waitFor(() => expect(calls).toEqual(['pause', 'resume']))
    act(() => useTransfersStore.getState().upsert([{ ...queued, paused: false }]))
    expect(await screen.findByRole('button', { name: 'Pause a.bin' })).toBeInTheDocument()
  })

  it('changes the priority of a waiting transfer', async () => {
    const bodies: unknown[] = []
    const queued = record('1', { name: 'a.bin', status: 'queued', bytesDone: 0 })
    server.use(
      http.patch('/api/v1/transfers/1', async ({ request }) => {
        bodies.push(await request.json())
        return HttpResponse.json({ ...queued, priority: -1 })
      }),
    )
    useTransfersStore.getState().upsert([queued])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    await choose(user, screen.getByRole('combobox', { name: 'Priority of a.bin' }), 'Low')

    await waitFor(() => expect(bodies).toEqual([{ priority: -1 }]))
    act(() => useTransfersStore.getState().upsert([{ ...queued, priority: -1 }]))
    expect(screen.getByRole('combobox', { name: 'Priority of a.bin' })).toHaveTextContent('Low')
  })

  it('switches between the tabs', async () => {
    useTransfersStore.getState().upsert([record('1'), record('2', { status: 'done' })])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    await user.click(screen.getByRole('tab', { name: 'Finished (1)' }))
    expect(useUiStore.getState().queueTab).toBe('finished')
    await user.click(screen.getByRole('tab', { name: 'Active (1)' }))

    expect(useUiStore.getState().queueTab).toBe('active')
    expect(screen.getByRole('listitem')).toHaveTextContent('1.bin')
  })

  it('lists the activity log and keeps it up to date', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    let reads = 0
    const entry = (id: string, kind: string, extra: Record<string, unknown> = {}) => ({ id, siteName: 'Prod', kind, path: `/${id}`, at: '2026-03-08T07:30:00Z', ...extra })
    server.use(
      http.get('/api/v1/workspaces/:id/activity', () => {
        reads++
        return HttpResponse.json([
          entry('a', 'mkdir'),
          entry('b', 'rename', { detail: '/b2' }),
          entry('c', 'delete', { errorCode: 'path_not_found' }),
          entry('d', 'checksum', { detail: 'sha256' }),
          entry('e', 'transfer', { detail: 'download:failed', errorCode: 'remote_unavailable' }),
          entry('f', 'transfer', { detail: 'copy:done' }),
          entry('g', 'transfer', { detail: 'odd' }),
          entry('h', 'compress', { detail: '/h.zip' }),
          entry('i', 'extract', { detail: '/i' }),
          entry('j', 'terminal', { detail: '1m5s' }),
        ])
      }),
    )
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    await renderWithProviders(<TransfersDock />)

    expect(reads).toBe(0)
    await user.click(screen.getByRole('tab', { name: 'Activity' }))
    const log = await screen.findByRole('list', { name: 'Activity log' })
    const rows = within(log).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('Created a folder')
    expect(rows[1]).toHaveTextContent('Renamed to /b2')
    expect(rows[2]).toHaveTextContent('Deleted')
    expect(rows[2]).toHaveTextContent('path_not_found')
    expect(rows[3]).toHaveTextContent('Checksum (sha256)')
    expect(rows[4]).toHaveTextContent('Download Failed')
    expect(rows[4]).toHaveTextContent('remote_unavailable')
    expect(rows[5]).toHaveTextContent('Copy Done')
    expect(rows[6]).toHaveTextContent('Upload Done')
    expect(rows[7]).toHaveTextContent('Compressed into /h.zip')
    expect(rows[8]).toHaveTextContent('Extracted into /i')
    expect(rows[9]).toHaveTextContent('Opened a terminal (1m5s)')
    expect(rows[0]).toHaveTextContent('Prod')
    expect(rows[0]).toHaveTextContent('/a')
    expect(rows[0]).toHaveTextContent('2026')

    const before = reads
    await act(() => vi.advanceTimersByTimeAsync(5500))
    expect(reads).toBeGreaterThan(before)
    vi.useRealTimers()
  })

  it('says when there is no activity or it cannot be loaded, in the interface language', async () => {
    server.use(http.get('/api/v1/workspaces/:id/activity', () => HttpResponse.json([])))
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />, 'es')

    await user.click(screen.getByRole('tab', { name: 'Actividad' }))
    expect(await screen.findByText('Todavía no hay actividad.')).toBeInTheDocument()

    server.use(http.get('/api/v1/workspaces/:id/activity', () => problem(500, 'internal_error')))
    await user.click(screen.getByRole('tab', { name: /Terminadas/ }))
    await user.click(screen.getByRole('tab', { name: 'Actividad' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('No se pudo cargar la actividad.'), { timeout: 3000 })
  })

  it('collapses', async () => {
    useTransfersStore.getState().upsert([record('1')])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    await user.click(screen.getByRole('button', { name: 'Hide transfers' }))

    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show transfers' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('speaks Spanish', async () => {
    useTransfersStore.getState().upsert([record('1', { status: 'interrupted' }), record('2', { status: 'queued', paused: true })])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />, 'es')

    expect(screen.getByRole('listitem')).toHaveTextContent('En pausa')
    expect(screen.getByRole('combobox', { name: 'Prioridad de 2.bin' })).toHaveTextContent('Normal')

    await user.click(screen.getByRole('tab', { name: 'Terminadas (1)' }))

    expect(screen.getByRole('listitem')).toHaveTextContent('Interrumpida')
  })
})
