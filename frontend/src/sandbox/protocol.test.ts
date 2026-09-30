import { describe, expect, it } from 'vitest'

import { isPageMessage, isRangeReply, isRenderMessage } from './protocol'

/**
 * Covers the messages between the explorer and the isolated page of the viewers (ADR-0018).
 *
 * Scenarios:
 * - a render message needs its type, a kind, a name, the bytes as an ArrayBuffer and the size; anything else is ignored
 * - the page accepts answers to its ranges with bytes or null
 * - the parent accepts only the ready, rendered, failed, range and key messages of the page
 */
describe('sandbox protocol', () => {
  it('recognizes render messages', () => {
    const message = { type: 'sentinel:render', kind: 'pdf', name: 'a.pdf', bytes: new ArrayBuffer(2), size: 2, theme: {}, labels: {} }
    expect(isRenderMessage(message)).toBe(true)
    expect(isRenderMessage({ ...message, bytes: 'text' })).toBe(false)
    expect(isRenderMessage({ ...message, kind: 3 })).toBe(false)
    expect(isRenderMessage({ ...message, name: undefined })).toBe(false)
    expect(isRenderMessage({ ...message, size: '2' })).toBe(false)
    expect(isRenderMessage({ ...message, type: 'other' })).toBe(false)
    expect(isRenderMessage(null)).toBe(false)
    expect(isRenderMessage('sentinel:render')).toBe(false)
  })

  it('recognizes the answers to ranges', () => {
    expect(isRangeReply({ type: 'sentinel:bytes', id: 3, bytes: new ArrayBuffer(4) })).toBe(true)
    expect(isRangeReply({ type: 'sentinel:bytes', id: 3, bytes: null })).toBe(true)
    expect(isRangeReply({ type: 'sentinel:bytes', id: '3', bytes: null })).toBe(false)
    expect(isRangeReply({ type: 'sentinel:bytes', id: 3, bytes: 'x' })).toBe(false)
    expect(isRangeReply({ type: 'other', id: 3, bytes: null })).toBe(false)
    expect(isRangeReply(null)).toBe(false)
  })

  it('recognizes the messages of the page', () => {
    expect(isPageMessage({ type: 'sentinel:ready' })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:rendered' })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:failed', reason: 'render' })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:range', id: 1, begin: 0, end: 10 })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:key', key: 'Escape' })).toBe(true)
    expect(isPageMessage({ type: 'sentinel:render' })).toBe(false)
    expect(isPageMessage(null)).toBe(false)
    expect(isPageMessage(42)).toBe(false)
  })
})
