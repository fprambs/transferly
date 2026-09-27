import { describe, expect, it } from 'vitest'

import type { SyncRow } from './compare'
import { hasCheckedDeletes, pending, planRowKey, type SyncPlanRow } from './plan'

/** Builds a bare row. */
function row(relativePath: string, action: SyncRow['action']): SyncRow {
  return { relativePath, action, reason: 'missing', source: null, target: null }
}

/** Builds a plan row with a status and whether it is checked. */
function planRow(action: SyncRow['action'], included: boolean, status: SyncPlanRow['status'] = 'pending'): SyncPlanRow {
  return { ...row('a.txt', action), included, status, error: null }
}

/**
 * Covers {@link planRowKey}.
 *
 * Scenarios:
 * - two rows of the same action and path share a key, and a different action or a different path each give a different one
 */
describe('planRowKey', () => {
  it('keys a row by its action and path', () => {
    expect(planRowKey(row('a.txt', 'update'))).toBe(planRowKey(row('a.txt', 'update')))
    expect(planRowKey(row('a.txt', 'update'))).not.toBe(planRowKey(row('a.txt', 'delete')))
    expect(planRowKey(row('a.txt', 'update'))).not.toBe(planRowKey(row('b.txt', 'update')))
  })
})

/**
 * Covers {@link pending}.
 *
 * Scenarios:
 * - a row still needs a run when it is checked and has not already finished
 * - an unchecked row, and one that already finished, do not
 */
describe('pending', () => {
  it('tells apart what still needs a run', () => {
    expect(pending(planRow('create', true))).toBe(true)
    expect(pending(planRow('create', false))).toBe(false)
    expect(pending(planRow('create', true, 'done'))).toBe(false)
    expect(pending(planRow('create', true, 'failed'))).toBe(true)
  })
})

/**
 * Covers {@link hasCheckedDeletes}.
 *
 * Scenarios:
 * - checked deletes are told apart from unchecked ones and from other actions
 * - a checked delete that already finished does not ask again
 */
describe('hasCheckedDeletes', () => {
  it('tells checked deletes apart from unchecked ones and from other actions', () => {
    expect(hasCheckedDeletes([planRow('delete', false)])).toBe(false)
    expect(hasCheckedDeletes([planRow('delete', true)])).toBe(true)
    expect(hasCheckedDeletes([planRow('create', true)])).toBe(false)
  })

  it('does not ask again about a delete that already finished', () => {
    expect(hasCheckedDeletes([planRow('delete', true, 'done')])).toBe(false)
  })
})
