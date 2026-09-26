import { describe, expect, it } from 'vitest'

import { auditActionKey, auditCategories } from './audit-actions'

/**
 * Covers {@link auditActionKey} and {@link auditCategories}.
 *
 * Scenarios:
 * - each known action has its own sentence and an unknown one gets the generic sentence
 * - every category selects a prefix that some known action starts with, except the one for everything
 */
describe('audit actions', () => {
  it('maps known actions and falls back for unknown ones', () => {
    expect(auditActionKey('member.removed')).toBe('memberRemoved')
    expect(auditActionKey('sites.imported')).toBe('sitesImported')
    expect(auditActionKey('something.new')).toBe('unknown')
  })

  it('offers categories that select something', () => {
    const actions = ['workspace.created', 'member.left', 'invitation.created', 'site.created', 'sites.imported', 'key.rotated', 'host_key.trusted']
    for (const category of auditCategories.filter((c) => c.prefix !== '')) {
      expect(actions.some((action) => action.startsWith(category.prefix)), category.key).toBe(true)
    }
    expect(auditCategories[0]).toEqual({ key: 'all', prefix: '' })
  })
})
