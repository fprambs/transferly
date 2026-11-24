import { describe, expect, it } from 'vitest'

import { returnPath } from './return-path'

/**
 * Covers which addresses the login screen goes back to.
 *
 * Scenarios:
 * - paths of the application keep their query and fragment
 * - nothing, relative paths, absolute and protocol-relative addresses, and paths that become one without tabs or newlines are refused
 */
describe('returnPath', () => {
  it.each(['/cli/authorize?code_challenge=abc&state=x', '/sites/s1?path=%2Fhome#top', '/'])('keeps %s', (path) => {
    expect(returnPath(path)).toBe(path)
  })

  it.each([undefined, '', 'cli/authorize', 'https://evil.example.com/', '//evil.example.com/x', '/\\evil.example.com', '/\t/evil.example.com', 'javascript:alert(1)'])(
    'refuses %s',
    (value) => {
      expect(returnPath(value)).toBeUndefined()
    },
  )
})
