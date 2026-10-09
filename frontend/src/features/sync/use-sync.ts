import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import type { Api } from '@/lib/api/use-api'

import { filesPrefix } from '../explorer/files-key'
import { listLocal, readLocalFile } from '../local/local-fs'

import { compareFiles } from './compare'
import { collectRelative, maxEachFiles, TooManyFiles, type CollectedFile } from './collect'
import { syncErrorKeyOf, type SyncErrorKey } from './errors'
import { newEnsuredDirectories, runSyncRow } from './run-row'
import { hasCheckedDeletes, hasCheckedOverwrites, pending, planRowKey, risky, type RowStatus, type SyncPlanRow } from './plan'
import type { SyncSide } from './sides'

/** Which of the two sides mirrors onto the other. */
export type SyncDirection = 'leftToRight' | 'rightToLeft'

/** A folder comparison as a whole and everything the interface needs to review and run it. */
export interface SyncPlan {
  /** Whether the two folders are still being listed. */
  loading: boolean
  /** Why listing failed, or `null` when it did not. */
  loadFailure: SyncErrorKey | 'tooMany' | null
  /** The rows of the plan, sorted by path. */
  rows: SyncPlanRow[]
  /** Which side mirrors onto the other. */
  direction: SyncDirection
  /** Chooses which side mirrors onto the other. */
  setDirection: (direction: SyncDirection) => void
  /** Checks or unchecks one row. */
  toggle: (row: SyncPlanRow) => void
  /** Whether any checked row is a delete, which needs the extra confirmation before running. */
  hasDeletes: boolean
  /** Whether any checked row replaces a newer file on the target, which needs the extra confirmation before running too. */
  hasOverwrites: boolean
  /** Reads the checksum of a row on both sides and settles it as equal or different, in place of guessing by time. */
  verify: (row: SyncPlanRow) => Promise<void>
  /** The row a checksum is being read for, or `null`. */
  verifying: string | null
  /** Runs every checked row in order; a row's failure does not stop the others. */
  run: () => Promise<void>
  /** Whether a run is in progress. */
  running: boolean
  /** Lists both folders again, forgetting how far any earlier run got. */
  refresh: () => void
}

/** How one row of the plan last ran, keyed by {@link planRowKey}. */
interface RunOutcome {
  /** How the row stands. */
  status: RowStatus
  /** Why it failed, once it has. */
  error: SyncErrorKey | null
}

/** Reads every file below a side's folder, relative to it. */
async function collectSide(api: Api, side: SyncSide, signal?: AbortSignal): Promise<CollectedFile[]> {
  const list = side.kind === 'remote' ? (path: string, abort?: AbortSignal) => api.files.list(side.workspaceId, side.siteId, path, abort) : (path: string) => listLocal(side.root, path)
  return collectRelative(list, side.path, maxEachFiles, signal)
}

/** A key that tells one side of a comparison apart from another, for the query cache. */
function sideKey(side: SyncSide): string {
  return side.kind === 'remote' ? `remote:${side.workspaceId}:${side.siteId}:${side.path}` : `local:${side.path}`
}

/** Invalidates the listings the explorer panes show for a side, so they pick up what a sync just changed. */
function invalidateSide(queryClient: ReturnType<typeof useQueryClient>, side: SyncSide): void {
  if (side.kind === 'remote') {
    void queryClient.invalidateQueries({ queryKey: filesPrefix(side.workspaceId, side.siteId) })
  } else {
    void queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] === 'local' })
  }
}

/** Reads the checksum of a path on a side. */
async function checksumOf(api: Api, side: SyncSide, relativePath: string): Promise<string> {
  const absolute = side.path === '/' ? `/${relativePath}` : `${side.path}/${relativePath}`
  if (side.kind === 'remote') {
    return (await api.files.checksum(side.workspaceId, side.siteId, absolute, 'sha256')).checksum
  }
  const buffer = await (await readLocalFile(side.root, absolute)).arrayBuffer()
  const digest = await crypto.subtle.digest('SHA-256', buffer)
  return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('')
}

/**
 * Compares the folders of two sides and lets the interface review and run the plan: choose the direction, check or uncheck rows, verify
 * an ambiguous one by checksum, and run every checked row through the ordinary transfer endpoints (ADR-0008). Everything here is derived
 * from what was compared plus the choices made about it, so a fresh comparison never needs to be reconciled with the last one by hand.
 * Nothing is saved: closing the dialog forgets the plan, the same way the transfer queue forgets a transfer once the tab that started it closes.
 */
export function useSyncPlan(api: Api, left: SyncSide, right: SyncSide): SyncPlan {
  const queryClient = useQueryClient()
  const leftQuery = useQuery({ queryKey: ['sync', sideKey(left)], queryFn: ({ signal }) => collectSide(api, left, signal) })
  const rightQuery = useQuery({ queryKey: ['sync', sideKey(right)], queryFn: ({ signal }) => collectSide(api, right, signal) })
  const [direction, setDirectionState] = useState<SyncDirection>('leftToRight')
  const [overrides, setOverrides] = useState<ReadonlyMap<string, boolean>>(new Map())
  const [includedOverrides, setIncludedOverrides] = useState<ReadonlyMap<string, boolean>>(new Map())
  const [outcomes, setOutcomes] = useState<ReadonlyMap<string, RunOutcome>>(new Map())
  const [verifying, setVerifying] = useState<string | null>(null)
  const [running, setRunning] = useState(false)

  const source = direction === 'leftToRight' ? left : right
  const target = direction === 'leftToRight' ? right : left
  const sourceFiles = direction === 'leftToRight' ? leftQuery.data : rightQuery.data
  const targetFiles = direction === 'leftToRight' ? rightQuery.data : leftQuery.data
  const compared = useMemo(() => (sourceFiles && targetFiles ? compareFiles(sourceFiles, targetFiles, overrides) : []), [sourceFiles, targetFiles, overrides])
  const rows = useMemo<SyncPlanRow[]>(
    () =>
      compared.map((row) => {
        const key = planRowKey(row)
        const outcome = outcomes.get(key)
        return { ...row, included: includedOverrides.get(key) ?? !risky(row), status: outcome?.status ?? 'pending', error: outcome?.error ?? null }
      }),
    [compared, includedOverrides, outcomes],
  )

  const loadError = leftQuery.error ?? rightQuery.error
  const loadFailure: SyncErrorKey | 'tooMany' | null = loadError === null ? null : loadError instanceof TooManyFiles ? 'tooMany' : syncErrorKeyOf(loadError)

  const setDirection = (next: SyncDirection): void => {
    setDirectionState(next)
    setOutcomes(new Map())
  }

  const toggle = (row: SyncPlanRow): void => setIncludedOverrides((previous) => new Map(previous).set(planRowKey(row), !row.included))

  const verify = async (row: SyncPlanRow): Promise<void> => {
    setVerifying(row.relativePath)
    try {
      const [fromSource, fromTarget] = await Promise.all([checksumOf(api, source, row.relativePath), checksumOf(api, target, row.relativePath)])
      setOverrides((previous) => new Map(previous).set(row.relativePath, fromSource === fromTarget))
    } finally {
      setVerifying(null)
    }
  }

  const settle = (key: string, outcome: RunOutcome): void => setOutcomes((previous) => new Map(previous).set(key, outcome))

  const run = async (): Promise<void> => {
    setRunning(true)
    const ensured = newEnsuredDirectories()
    for (const row of rows) {
      const key = planRowKey(row)
      if (!pending(row)) {
        continue
      }
      settle(key, { status: 'running', error: null })
      try {
        await runSyncRow(api, source, target, row, ensured)
        settle(key, { status: 'done', error: null })
      } catch (error) {
        settle(key, { status: 'failed', error: syncErrorKeyOf(error) })
      }
    }
    invalidateSide(queryClient, left)
    invalidateSide(queryClient, right)
    setRunning(false)
  }

  const refresh = (): void => {
    setOutcomes(new Map())
    void leftQuery.refetch()
    void rightQuery.refetch()
  }

  return { loading: leftQuery.isPending || rightQuery.isPending, loadFailure, rows, direction, setDirection, toggle, hasDeletes: hasCheckedDeletes(rows), hasOverwrites: hasCheckedOverwrites(rows), verifying, verify, running, run, refresh }
}
