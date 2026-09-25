import { describe, expect, it } from 'vitest'

import type { Workspace } from '@/lib/api'

import { pickWorkspace } from './use-workspace'

/** Builds a workspace. */
function workspace(id: string, kind: Workspace['kind']): Workspace {
  return { id, name: id, kind, role: 'owner', createdAt: '2026-01-01T00:00:00Z' }
}

/**
 * Covers {@link pickWorkspace}.
 *
 * Scenarios:
 * - the personal workspace wins even when it is not the first
 * - without a personal workspace the first one is used
 * - no workspaces means nothing to pick
 */
describe('pickWorkspace', () => {
  it('prefers the personal workspace', () => {
    expect(pickWorkspace([workspace('team', 'team'), workspace('me', 'personal')])?.id).toBe('me')
  })

  it('falls back to the first workspace', () => {
    expect(pickWorkspace([workspace('a', 'team'), workspace('b', 'team')])?.id).toBe('a')
  })

  it('returns undefined when there are none', () => {
    expect(pickWorkspace([])).toBeUndefined()
  })
})
