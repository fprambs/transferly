import { describe, expect, it } from 'vitest'

import { isPageMessage, isRenderMessage } from './protocol'

/**
 * Covers the messages between the explorer and the isolated page of the viewers (ADR-0018).
 *
 * Scenarios:
 * - a render message needs its type, a kind, a name and the bytes as an ArrayBuffer; anything else is ignored
 * - the parent accepts only the ready, rendered and failed messages of the page
 */
describe('sandbox protocol', () => {
  it('recognizes render messages', () => {
    const message = { type: 'sentinel:render', kind: 'pdf', name: 'a.pdf', bytes: new ArrayBuffer(2), theme: {} }
    expect(isRenderMessage(message)).toBe(true)
    expect(isRenderMessage({ ...message, bytes: 'text' })).toBe(false)
    expect(isRenderMessage({ ...message, kind: 3 })).toBe(false)
    expect(isRenderMessage({ ...message, name: undefined })).toBe(false)
    expect(isRenderMessage({ ...message, type: 'other' })).toBe(false)
    expect(isRenderMessage(null)).toBe(false)
    expect(isRenderMessage('sentinel:render')).toBe(false)
  })

  it('recognizes the messages of the page', () => {
    expect(isPageMessage({ type: 'sentinel:ready' })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:rendered' })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:failed', reason: 'render' })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:render' })).toBe(false)
    expect(isPageMessage(null)).toBe(false)
    expect(isPageMessage(42)).toBe(false)
  })
})
