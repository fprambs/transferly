import { afterEach, describe, expect, it, vi } from 'vitest'

import { editorNonce, nonceStyles, styleNonce } from './csp'

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

/** Adds a style element with a rule under root. */
function style(root: HTMLElement, rule: string): HTMLStyleElement {
  const element = document.createElement('style')
  element.textContent = rule
  root.append(element)
  return element
}

/**
 * Covers {@link nonceStyles}.
 *
 * Scenarios:
 * - with a nonce, the style elements already under the root and those added later, at any depth, get it and keep their rules; after
 *   it stops watching, new ones are left alone
 * - without a nonce nothing is touched
 */
describe('nonce styles', () => {
  afterEach(() => document.head.querySelectorAll('meta[name="csp-nonce"]').forEach((tag) => tag.remove()))

  it('gives the nonce to the styles under a root', async () => {
    meta('abc123')
    const root = document.createElement('div')
    const before = style(root, '.a { color: red; }')
    const stop = nonceStyles(root)
    expect(before.nonce).toBe('abc123')
    expect(before.textContent).toBe('.a { color: red; }')
    const inner = document.createElement('div')
    root.append(inner)
    const later = style(inner, '.b { color: blue; }')
    await vi.waitFor(() => expect(later.nonce).toBe('abc123'))
    expect(later.textContent).toBe('.b { color: blue; }')
    stop()
    const after = style(root, '.c {}')
    await Promise.resolve()
    expect(after.nonce).toBe('')
  })

  it('leaves the styles alone without a nonce', () => {
    const root = document.createElement('div')
    const element = style(root, '.a {}')
    nonceStyles(root)()
    expect(element.nonce).toBe('')
  })
})
