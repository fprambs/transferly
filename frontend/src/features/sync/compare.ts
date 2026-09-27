import type { RemoteEntry } from '@/lib/api'

import type { CollectedFile } from './collect'

/** How far apart two modification times may be and still count as the same instant, in milliseconds. Servers report times to the
 * second and clocks drift a little, so a small gap alone does not mean the files differ (ADR-0008). */
export const timeToleranceMs = 60_000

/** What a row of the plan asks for. */
export type SyncAction = 'create' | 'update' | 'delete'

/** Why a row was told apart from an equal file. */
export type SyncReason = 'missing' | 'size' | 'time' | 'checksum'

/** One file that differs between the two folders, or exists on only one side. */
export interface SyncRow {
  /** Path of the file below the compared folders, without a leading slash. */
  relativePath: string
  /** What running this row does. */
  action: SyncAction
  /** Why it was told apart from an equal file. */
  reason: SyncReason
  /** The file on the side rows are made from, or `null` when it only exists on the other side. */
  source: RemoteEntry | null
  /** The file on the side rows are made to match, or `null` when it does not exist there yet. */
  target: RemoteEntry | null
}

/** Tells whether two ISO instants are close enough to count as the same modification time. */
function timesAgree(a: string, b: string): boolean {
  return Math.abs(new Date(a).getTime() - new Date(b).getTime()) <= timeToleranceMs
}

/** Compares two files that exist on both sides and returns their row, or `null` when they already agree. */
function compareBoth(relativePath: string, from: RemoteEntry, to: RemoteEntry, override: boolean | undefined): SyncRow | null {
  if (from.size !== to.size) {
    return { relativePath, action: 'update', reason: 'size', source: from, target: to }
  }
  if (override === false) {
    return { relativePath, action: 'update', reason: 'checksum', source: from, target: to }
  }
  if (override === true || timesAgree(from.modifiedAt, to.modifiedAt)) {
    return null
  }
  return { relativePath, action: 'update', reason: 'time', source: from, target: to }
}

/**
 * Compares the files of two folders, matched by their path relative to each, and returns one row per file that differs or
 * exists on only one side; files that agree produce no row. `overrides` gives the verdict of a manual checksum check for a
 * path, `true` for equal and `false` for different, and wins over the time comparison.
 */
export function compareFiles(source: readonly CollectedFile[], target: readonly CollectedFile[], overrides: ReadonlyMap<string, boolean> = new Map()): SyncRow[] {
  const fromSource = new Map(source.map((file) => [file.relativePath, file.entry]))
  const fromTarget = new Map(target.map((file) => [file.relativePath, file.entry]))
  const rows: SyncRow[] = []
  for (const relativePath of new Set([...fromSource.keys(), ...fromTarget.keys()])) {
    const hasSource = fromSource.has(relativePath)
    const hasTarget = fromTarget.has(relativePath)
    if (hasSource && !hasTarget) {
      rows.push({ relativePath, action: 'create', reason: 'missing', source: fromSource.get(relativePath) as RemoteEntry, target: null })
      continue
    }
    if (!hasSource && hasTarget) {
      rows.push({ relativePath, action: 'delete', reason: 'missing', source: null, target: fromTarget.get(relativePath) as RemoteEntry })
      continue
    }
    const row = compareBoth(relativePath, fromSource.get(relativePath) as RemoteEntry, fromTarget.get(relativePath) as RemoteEntry, overrides.get(relativePath))
    if (row !== null) {
      rows.push(row)
    }
  }
  return rows.sort((a, b) => a.relativePath.localeCompare(b.relativePath))
}
