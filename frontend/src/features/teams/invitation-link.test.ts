import { describe, expect, it } from 'vitest'

import { invitationLink } from './invitation-link'

/**
 * Covers {@link invitationLink}.
 *
 * Scenarios:
 * - the token goes after a hash, never in the path or the query
 */
describe('invitationLink', () => {
  it('puts the token in the fragment', () => {
    expect(invitationLink('https://sentinel.example', 'abc_DEF-123')).toBe('https://sentinel.example/invite#abc_DEF-123')
  })
})
