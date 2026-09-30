import type { SyncRow } from './compare'
import type { SyncErrorKey } from './errors'

/** How a row of the plan stands: waiting to run, running, done, or failed with the reason. */
export type RowStatus = 'pending' | 'running' | 'done' | 'failed'

/** One row of the plan, with whether it will run and how it went. */
export interface SyncPlanRow extends SyncRow {
  /** Whether the row is checked to run. Create and update rows start checked; delete rows start unchecked, because nothing deletes by
   * default (ADR-0008), and so do updates that would replace a newer file on the target. */
  included: boolean
  /** How the row stands in the current run. */
  status: RowStatus
  /** Why the row failed, once it has. */
  error: SyncErrorKey | null
}

/** Identifies a row across two comparisons, so a checked choice or a run's status can be kept for the same file even after the plan is
 * compared again; a one-sided row's identity changes with its action, because swapping direction genuinely turns a create into a delete. */
export function planRowKey(row: Pick<SyncRow, 'action' | 'relativePath'>): string {
  return `${row.action}:${row.relativePath}`
}

/** Tells whether a row still needs a run: it is checked and has not already finished. */
export function pending(row: SyncPlanRow): boolean {
  return row.included && row.status !== 'done'
}

/** Tells whether a row loses data when it runs, a delete or an update over a newer file, so it starts unchecked and asks again. */
export function risky(row: SyncRow): boolean {
  return row.action === 'delete' || row.newerTarget
}

/** Tells whether any row still waiting to run replaces a newer file on the target, which needs the extra confirmation before a run starts. */
export function hasCheckedOverwrites(rows: readonly SyncPlanRow[]): boolean {
  return rows.some((row) => row.newerTarget && pending(row))
}

/** Tells whether any row still waiting to run is a delete, which needs the extra confirmation before a run starts; a delete that already
 * ran does not ask again. */
export function hasCheckedDeletes(rows: readonly SyncPlanRow[]): boolean {
  return rows.some((row) => row.action === 'delete' && pending(row))
}
