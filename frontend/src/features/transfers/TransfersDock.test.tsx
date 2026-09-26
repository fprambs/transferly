import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { TransferRecord } from '@/lib/transport'
import { problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'
import { useUiStore } from '@/state/ui-store'

import { TransfersDock } from './TransfersDock'
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
 * - the tabs switch both ways and the choice is kept in the interface state
 * - the panel collapses and the texts follow the interface language
 */
describe('TransfersDock', () => {
  beforeEach(() => {
    useTransfersStore.getState().reset()
    useUiStore.setState({ queueOpen: true, queueTab: 'active' })
  })

  it('says when there are no transfers', async () => {
    await renderWithProviders(<TransfersDock />)

    expect(screen.getByText('No transfers')).toBeInTheDocument()
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

  it('collapses', async () => {
    useTransfersStore.getState().upsert([record('1')])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />)

    await user.click(screen.getByRole('button', { name: 'Hide transfers' }))

    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show transfers' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('speaks Spanish', async () => {
    useTransfersStore.getState().upsert([record('1', { status: 'interrupted' })])
    const user = userEvent.setup()
    await renderWithProviders(<TransfersDock />, 'es')

    await user.click(screen.getByRole('tab', { name: 'Terminadas (1)' }))

    expect(screen.getByRole('listitem')).toHaveTextContent('Interrumpida')
  })
})
