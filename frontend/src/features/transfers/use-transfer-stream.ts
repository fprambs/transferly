import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'

import { useApi } from '@/lib/api/use-api'
import { useTransport, type ServerEvent } from '@/lib/transport'

import { vaultKey } from '../vault/use-vault'

import { useTransfersStore } from './transfers-store'

/** Waits, in milliseconds, before each reconnection attempt; the last one repeats. */
const retryDelays = [1000, 2000, 5000, 10_000, 30_000]

/** Most transfers loaded from the history when the stream opens. */
const historyLimit = 100

/** Returns the wait before the reconnection attempt that follows `failures` failed ones. */
export function retryDelay(failures: number): number {
  return retryDelays[Math.min(failures, retryDelays.length - 1)]
}

/** Reads the clock, in milliseconds; kept apart so the components stay free of impure calls. */
function nowMs(): number {
  return Date.now()
}

/** Applies an event of the stream to the transfer list; a vault lock is reported through `onVaultLocked`. */
export function applyEvent(event: ServerEvent, onVaultLocked: () => void): void {
  const store = useTransfersStore.getState()
  if (event.type === 'snapshot') {
    store.upsert(event.data.transfers)
  } else if (event.type === 'transfer.state') {
    store.upsert([event.data])
  } else if (event.type === 'transfer.progress') {
    store.progress(event.data.id, event.data.bytesDone, event.data.totalBytes, nowMs())
  } else {
    onVaultLocked()
  }
}

/**
 * Keeps the transfer list in step with the server for as long as the component that calls it is mounted.
 *
 * It opens the event stream, loads the history whenever the stream opens, reconnects with growing waits when it drops,
 * and refreshes the vault state when the server reports a lock.
 */
export function useTransferStream(): void {
  const transport = useTransport()
  const api = useApi()
  const queryClient = useQueryClient()

  useEffect(() => {
    let failures = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    const connect = (): (() => void) =>
      transport.events({
        onEvent: (event) => applyEvent(event, () => void queryClient.invalidateQueries({ queryKey: vaultKey })),
        onOpen: () => {
          failures = 0
          void api.transfers
            .list({ limit: historyLimit })
            .then((records) => useTransfersStore.getState().upsert(records))
            .catch(() => undefined)
        },
        onClose: () => {
          timer = setTimeout(() => {
            stop = connect()
          }, retryDelay(failures++))
        },
      })
    let stop = connect()
    return () => {
      clearTimeout(timer)
      stop()
    }
  }, [transport, api, queryClient])
}
