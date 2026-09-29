import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'

import type { OpenConnections } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { useTransport, type ServerEvent, type TransferRecord } from '@/lib/transport'

import { useConnectionLog } from '../connlog/connection-log-store'
import { usePrompts } from '../prompts/prompts-store'
import { openConnectionsKey, withState } from '../sites/use-open-connections'
import { vaultKey } from '../vault/use-vault'

import { useUiStore } from '@/state/ui-store'

import { activeStatuses, useTransfersStore } from './transfers-store'

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

/** Tells whether a record is a transfer that just became active: it was unknown or not running before. */
function startedNow(record: TransferRecord): boolean {
  const known = useTransfersStore.getState().items[record.id] as { record: TransferRecord } | undefined
  return activeStatuses.includes(record.status) && (known === undefined || !activeStatuses.includes(known.record.status))
}

/** Applies an event of the stream to the transfer list, to the connection log of its site, or to the prompts waiting for an answer; a vault
 * lock is reported through `onVaultLocked`. */
export function applyEvent(event: ServerEvent, onVaultLocked: () => void): void {
  const store = useTransfersStore.getState()
  if (event.type === 'snapshot') {
    const started = event.data.transfers.some(startedNow)
    store.upsert(event.data.transfers)
    if (started) {
      useUiStore.getState().showActiveTransfers()
    }
  } else if (event.type === 'transfer.state') {
    const started = startedNow(event.data)
    store.upsert([event.data])
    if (started) {
      useUiStore.getState().showActiveTransfers()
    }
  } else if (event.type === 'transfer.progress') {
    store.progress(event.data.id, event.data.bytesDone, event.data.totalBytes, nowMs())
  } else if (event.type === 'connection.log') {
    useConnectionLog.getState().add(event.data)
  } else if (event.type === 'connection.prompt') {
    usePrompts.getState().add(event.data)
  } else if (event.type === 'connection.prompt_closed') {
    usePrompts.getState().close(event.data.id)
  } else if (event.type === 'vault.locked') {
    onVaultLocked()
  }
}

/**
 * Keeps the transfer list in step with the server for as long as the component that calls it is mounted.
 *
 * It opens the event stream, loads the history whenever the stream opens, reconnects with growing waits when it drops,
 * refreshes the vault state when the server reports a lock, reloads the folder listings when a copy between sites ends, and keeps the
 * connected sites current, reloading them whenever the stream opens.
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
        onEvent: (event) => {
          applyEvent(event, () => void queryClient.invalidateQueries({ queryKey: vaultKey }))
          if (event.type === 'connection.state') {
            const { workspaceId, siteId, open } = event.data
            queryClient.setQueryData(openConnectionsKey(workspaceId), (answer: OpenConnections | undefined) => withState(answer, siteId, open))
          }
          if (event.type === 'transfer.state' && event.data.direction === 'copy' && event.data.status === 'done') {
            void queryClient.invalidateQueries({ queryKey: ['workspaces', event.data.workspaceId, 'files'] })
          }
        },
        onOpen: () => {
          failures = 0
          void queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] === 'workspaces' && query.queryKey[2] === 'connections' })
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
