import { create } from 'zustand'

import type { TransferRecord } from '@/lib/transport'

/** A transfer as the interface tracks it: the server's record plus a measured speed. */
export interface TransferItem {
  /** The record as the server last reported it. */
  record: TransferRecord
  /** Smoothed speed in bytes per second; zero until two progress reports arrived. */
  speed: number
  /** When the last progress report arrived, in milliseconds since the epoch. */
  sampledAt: number
}

/** How much of a new speed sample is mixed into the smoothed speed. */
const smoothing = 0.3

/** Most transfers kept in memory; older ones are dropped. */
export const maxTransfers = 200

/** State and actions of the transfer list. */
export interface TransfersState {
  /** Transfers by identifier. */
  items: Record<string, TransferItem>
  /** Adds or updates transfers from the server, keeping the speed already measured. */
  upsert: (records: readonly TransferRecord[]) => void
  /** Applies a progress report received at `now`. */
  progress: (id: string, bytesDone: number, totalBytes: number, now: number) => void
  /** Forgets a transfer. */
  forget: (id: string) => void
  /** Forgets every transfer. */
  reset: () => void
}

/** Keeps the newest transfers when there are more than {@link maxTransfers}. */
function trim(items: Record<string, TransferItem>): Record<string, TransferItem> {
  const entries = Object.entries(items)
  if (entries.length <= maxTransfers) {
    return items
  }
  const newest = entries.sort(([, a], [, b]) => b.record.createdAt.localeCompare(a.record.createdAt)).slice(0, maxTransfers)
  return Object.fromEntries(newest)
}

/** Store of the transfers the server reports, fed by its event stream. */
export const useTransfersStore = create<TransfersState>()((set) => ({
  items: {},
  upsert: (records) =>
    set((state) => {
      const items = { ...state.items }
      for (const record of records) {
        const known = items[record.id] as TransferItem | undefined
        items[record.id] = { record, speed: known?.speed ?? 0, sampledAt: known?.sampledAt ?? 0 }
      }
      return { items: trim(items) }
    }),
  progress: (id, bytesDone, totalBytes, now) =>
    set((state) => {
      const known = state.items[id] as TransferItem | undefined
      if (known === undefined) {
        return state
      }
      const elapsed = now - known.sampledAt
      const sample = known.sampledAt > 0 && elapsed > 0 ? ((bytesDone - known.record.bytesDone) / elapsed) * 1000 : 0
      const speed = known.speed === 0 ? sample : known.speed * (1 - smoothing) + sample * smoothing
      return { items: { ...state.items, [id]: { record: { ...known.record, bytesDone, totalBytes }, speed: Math.max(speed, 0), sampledAt: now } } }
    }),
  forget: (id) =>
    set((state) => {
      const items = { ...state.items }
      delete items[id]
      return { items }
    }),
  reset: () => set({ items: {} }),
}))

/** Statuses of a transfer that is still going or waiting. */
export const activeStatuses: readonly TransferRecord['status'][] = ['queued', 'running']

/** Tells whether a transfer is still going or waiting. */
export function isActive(item: TransferItem): boolean {
  return activeStatuses.includes(item.record.status)
}

/** Lists transfers newest first. */
export function newestFirst(items: Record<string, TransferItem>): TransferItem[] {
  return Object.values(items).sort((a, b) => b.record.createdAt.localeCompare(a.record.createdAt))
}

/** Estimates the seconds a running transfer still needs, or `undefined` when its size or speed is unknown. */
export function etaSeconds(item: TransferItem): number | undefined {
  const total = item.record.totalBytes
  if (total === undefined || item.speed <= 0) {
    return undefined
  }
  return Math.max(Math.ceil((total - item.record.bytesDone) / item.speed), 0)
}
