import { beforeEach, describe, expect, it } from 'vitest'

import type { TransferRecord } from '@/lib/transport'

import { etaSeconds, isActive, maxTransfers, newestFirst, useTransfersStore } from './transfers-store'

/** Builds a transfer record. */
function record(id: string, overrides: Partial<TransferRecord> = {}): TransferRecord {
  return {
    id,
    workspaceId: 'w1',
    siteName: 'Prod',
    direction: 'upload',
    path: `/${id}`,
    name: id,
    status: 'running',
    totalBytes: 1000,
    bytesDone: 0,
    attempts: 1,
    createdAt: `2026-01-01T00:00:${id.padStart(2, '0')}Z`,
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

/**
 * Covers {@link useTransfersStore} and its helpers.
 *
 * Scenarios:
 * - upserting adds transfers and updates known ones without losing their measured speed
 * - progress updates the bytes, smooths the speed over reports, ignores unknown transfers and never goes below zero
 * - a first report only starts the clock and a report with no elapsed time keeps the speed
 * - forgetting and resetting remove transfers
 * - only the newest transfers are kept
 * - helpers tell active transfers, order them newest first and estimate the time left
 */
describe('transfers store', () => {
  beforeEach(() => {
    useTransfersStore.getState().reset()
  })

  it('adds and updates transfers', () => {
    useTransfersStore.getState().upsert([record('1'), record('2')])
    useTransfersStore.getState().upsert([record('1', { status: 'done' })])

    const { items } = useTransfersStore.getState()
    expect(Object.keys(items)).toEqual(['1', '2'])
    expect(items['1'].record.status).toBe('done')
  })

  it('measures and smooths the speed', () => {
    const store = useTransfersStore.getState()
    store.upsert([record('1')])

    store.progress('1', 100, 1000, 10_000)
    expect(useTransfersStore.getState().items['1'].speed).toBe(0)

    store.progress('1', 300, 1000, 11_000)
    expect(useTransfersStore.getState().items['1'].speed).toBe(200)

    store.progress('1', 700, 1000, 12_000)
    const item = useTransfersStore.getState().items['1']
    expect(item.speed).toBeCloseTo(200 * 0.7 + 400 * 0.3)
    expect(item.record.bytesDone).toBe(700)
    expect(item.record.totalBytes).toBe(1000)
  })

  it('keeps the speed when no time passed, ignores unknown transfers and never goes negative', () => {
    const store = useTransfersStore.getState()
    store.upsert([record('1', { bytesDone: 500 })])
    store.progress('1', 600, 1000, 1000)
    store.progress('1', 700, 1000, 2000)
    const before = useTransfersStore.getState().items['1'].speed

    store.progress('1', 800, 1000, 2000)
    expect(useTransfersStore.getState().items['1'].speed).toBeCloseTo(before * 0.7)

    store.progress('missing', 1, 1, 3000)
    expect(useTransfersStore.getState().items['missing']).toBeUndefined()

    store.progress('1', 10, 1000, 3000)
    expect(useTransfersStore.getState().items['1'].speed).toBeGreaterThanOrEqual(0)
  })

  it('forgets one transfer and resets all', () => {
    const store = useTransfersStore.getState()
    store.upsert([record('1'), record('2')])

    store.forget('1')
    expect(Object.keys(useTransfersStore.getState().items)).toEqual(['2'])

    store.reset()
    expect(useTransfersStore.getState().items).toEqual({})
  })

  it('keeps only the newest transfers', () => {
    const records = Array.from({ length: maxTransfers + 5 }, (_, index) =>
      record(String(index), { createdAt: `2026-01-01T00:${String(Math.floor(index / 60)).padStart(2, '0')}:${String(index % 60).padStart(2, '0')}Z` }),
    )

    useTransfersStore.getState().upsert(records)

    const ids = Object.keys(useTransfersStore.getState().items)
    expect(ids).toHaveLength(maxTransfers)
    expect(ids).not.toContain('0')
    expect(ids).toContain(String(maxTransfers + 4))
  })

  it('tells active transfers, orders them and estimates the time left', () => {
    useTransfersStore.getState().upsert([record('1', { status: 'queued' }), record('2', { status: 'failed' }), record('3', { totalBytes: undefined })])
    const { items } = useTransfersStore.getState()

    expect(isActive(items['1'])).toBe(true)
    expect(isActive(items['2'])).toBe(false)
    expect(newestFirst(items).map((item) => item.record.id)).toEqual(['3', '2', '1'])

    expect(etaSeconds({ ...items['1'], speed: 100, record: { ...items['1'].record, bytesDone: 400 } })).toBe(6)
    expect(etaSeconds({ ...items['1'], speed: 0 })).toBeUndefined()
    expect(etaSeconds({ ...items['3'], speed: 100 })).toBeUndefined()
    expect(etaSeconds({ ...items['1'], speed: 100, record: { ...items['1'].record, bytesDone: 2000 } })).toBe(0)
  })
})
