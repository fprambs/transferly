import { act, cleanup, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { FakeSocket, fakeSocketTransport } from '@/test/fake-socket'
import { mockVault } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { useUiStore } from '@/state/ui-store'
import { useVaultStatus } from '../vault/use-vault'

import { useTransfersStore } from './transfers-store'
import { retryDelay, useTransferStream } from './use-transfer-stream'

/** A transfer record for the history. */
const record = {
  id: 't1',
  workspaceId: 'w1',
  siteName: 'Prod',
  direction: 'download',
  path: '/a.txt',
  name: 'a.txt',
  status: 'running',
  totalBytes: 1000,
  bytesDone: 0,
  attempts: 1,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
} as const

/** Mounts the hook and shows how many transfers the store holds. */
function Probe() {
  useTransferStream()
  useVaultStatus()
  const count = useTransfersStore((state) => Object.keys(state.items).length)
  return <p>{count} transfers</p>
}

/** Renders the probe with a transport that opens fake sockets. */
async function renderProbe() {
  await renderWithProviders(<Probe />, 'en', fakeSocketTransport())
}

/**
 * Covers {@link useTransferStream} and {@link retryDelay}.
 *
 * Scenarios:
 * - opening the stream loads the history, and a failing history load is ignored
 * - snapshots and state events add transfers and progress events update them
 * - a vault lock event refreshes the vault state
 * - a transfer that starts opens a closed queue on its active tab, while updates of a running one, finished ones and progress leave it as the user set it
 * - a dropped stream reconnects after a growing wait that is capped, and the wait starts again after a successful open
 * - unmounting closes the stream and cancels a pending reconnection
 */
describe('useTransferStream', () => {
  beforeEach(() => {
    useTransfersStore.getState().reset()
    mockVault()
    server.use(http.get('/api/v1/transfers', () => HttpResponse.json([])))
  })

  afterEach(() => {
    FakeSocket.reset()
    vi.useRealTimers()
  })

  it('loads the history when the stream opens', async () => {
    server.use(http.get('/api/v1/transfers', () => HttpResponse.json([record])))
    await renderProbe()

    act(() => FakeSocket.opened[0].onopen?.())

    expect(await screen.findByText('1 transfers')).toBeInTheDocument()
  })

  it('ignores a failing history load', async () => {
    let calls = 0
    server.use(http.get('/api/v1/transfers', () => (calls++, new HttpResponse(null, { status: 500 }))))
    await renderProbe()

    act(() => FakeSocket.opened[0].onopen?.())

    await waitFor(() => expect(calls).toBe(1))
    expect(screen.getByText('0 transfers')).toBeInTheDocument()
  })

  it('applies snapshots, state changes and progress', async () => {
    await renderProbe()
    const socket = FakeSocket.opened[0]

    act(() => socket.emit('snapshot', { transfers: [record] }))
    expect(await screen.findByText('1 transfers')).toBeInTheDocument()

    act(() => socket.emit('transfer.state', { ...record, id: 't2' }))
    expect(await screen.findByText('2 transfers')).toBeInTheDocument()

    act(() => socket.emit('transfer.progress', { id: 't1', bytesDone: 500, totalBytes: 1000 }))
    expect(useTransfersStore.getState().items.t1.record.bytesDone).toBe(500)
  })

  it('opens the queue when a transfer starts and leaves it alone otherwise', async () => {
    await renderProbe()
    const socket = FakeSocket.opened[0]
    const closed = () => useUiStore.setState({ queueOpen: false, queueTab: 'finished' })

    closed()
    act(() => socket.emit('transfer.state', { ...record, status: 'done' }))
    expect(useUiStore.getState()).toMatchObject({ queueOpen: false, queueTab: 'finished' })

    act(() => socket.emit('transfer.state', { ...record, id: 't2', status: 'queued' }))
    expect(useUiStore.getState()).toMatchObject({ queueOpen: true, queueTab: 'active' })

    closed()
    act(() => socket.emit('transfer.state', { ...record, id: 't2', status: 'running' }))
    act(() => socket.emit('transfer.progress', { id: 't2', bytesDone: 5, totalBytes: 10 }))
    expect(useUiStore.getState()).toMatchObject({ queueOpen: false, queueTab: 'finished' })

    act(() => socket.emit('snapshot', { transfers: [{ ...record, id: 't3', status: 'running' }] }))
    expect(useUiStore.getState()).toMatchObject({ queueOpen: true, queueTab: 'active' })

    closed()
    act(() => socket.emit('snapshot', { transfers: [{ ...record, id: 't3', status: 'running' }, { ...record, id: 't1', status: 'done' }] }))
    expect(useUiStore.getState()).toMatchObject({ queueOpen: false, queueTab: 'finished' })
  })

  it('refreshes the vault state when the vault is locked', async () => {
    let reads = 0
    server.use(http.get('/api/v1/vault', () => (reads++, HttpResponse.json({ initialized: true, unlocked: true }))))
    await renderProbe()
    await waitFor(() => expect(reads).toBe(1))

    act(() => FakeSocket.opened[0].emit('vault.locked', null))

    await waitFor(() => expect(reads).toBe(2))
  })

  it('reconnects with growing waits and starts again after opening', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderProbe()

    act(() => FakeSocket.opened[0].onclose?.())
    expect(FakeSocket.opened).toHaveLength(1)
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(FakeSocket.opened).toHaveLength(2)

    act(() => FakeSocket.opened[1].onclose?.())
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(FakeSocket.opened).toHaveLength(2)
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(FakeSocket.opened).toHaveLength(3)

    act(() => FakeSocket.opened[2].onopen?.())
    act(() => FakeSocket.opened[2].onclose?.())
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(FakeSocket.opened).toHaveLength(4)
  })

  it('caps the wait between attempts', () => {
    expect([0, 1, 2, 3, 4, 5, 50].map(retryDelay)).toEqual([1000, 2000, 5000, 10_000, 30_000, 30_000, 30_000])
  })

  it('closes the stream and cancels a pending reconnection when unmounted', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderProbe()
    const first = FakeSocket.opened[0]

    act(() => first.onclose?.())
    cleanup()
    await act(() => vi.advanceTimersByTimeAsync(60_000))

    expect(FakeSocket.opened).toHaveLength(1)
  })

  it('closes the socket when unmounted', async () => {
    await renderProbe()
    const socket = FakeSocket.opened[0]

    cleanup()

    expect(socket.closed).toBe(true)
  })
})
