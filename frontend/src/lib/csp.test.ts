import { afterEach, describe, expect, it } from 'vitest'

import { editorNonce, styleNonce } from './csp'

/** Puts a nonce meta tag in the document. */
function meta(content: string): void {
  const tag = document.createElement('meta')
  tag.name = 'csp-nonce'
  tag.content = content
  document.head.append(tag)
}

/**
 * Covers {@link styleNonce} and {@link editorNonce}.
 *
 * Scenarios:
 * - the nonce the server wrote into the page is read, and CodeMirror gets it
 * - no meta tag, an empty one and the placeholder of development give no nonce, and CodeMirror gets nothing
 */
describe('style nonce', () => {
  afterEach(() => document.head.querySelectorAll('meta[name="csp-nonce"]').forEach((tag) => tag.remove()))

  it('reads the nonce of the page', () => {
    meta('abc123')
    expect(styleNonce()).toBe('abc123')
    expect(editorNonce()).not.toEqual([])
  })

  it('has no nonce without one', () => {
    expect(styleNonce()).toBeUndefined()
    meta('')
    expect(styleNonce()).toBeUndefined()
    document.head.querySelector('meta[name="csp-nonce"]')?.remove()
    meta('__CSP_NONCE__')
    expect(styleNonce()).toBeUndefined()
    expect(editorNonce()).toEqual([])
  })
})
