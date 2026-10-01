import { act, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { bytesSource } from '@/test/preview'

import { SandboxFrame } from './SandboxFrame'

/** Renders a frame and captures what it posts to its page. */
function renderFrame(props: Partial<Parameters<typeof SandboxFrame>[0]> = {}) {
  const onFailed = vi.fn()
  render(<SandboxFrame kind="pdf" name="a.pdf" bytes={new Uint8Array([1, 2])} title="Frame" onFailed={onFailed} {...props} />)
  const frame = screen.getByTitle('Frame') as HTMLIFrameElement
  const sent: Array<Record<string, unknown>> = []
  ;(frame.contentWindow as Window).postMessage = (message: Record<string, unknown>) => void sent.push(message)
  const fromPage = (data: unknown) => act(() => window.dispatchEvent(new MessageEvent('message', { data, source: frame.contentWindow })))
  return { frame, sent, fromPage, onFailed }
}

/**
 * Covers {@link SandboxFrame} (ADR-0018).
 *
 * Scenarios:
 * - once ready, the page gets the first bytes, the size of the file (or of the bytes without a source), the theme and the labels
 * - ranges are read from the source and handed back; without a source, over 4 MiB or when the read fails, the answer is null
 * - Escape and the arrows pressed inside the page are pressed again on the frame
 * - a failure is reported with its reason, or an empty one
 */
describe('SandboxFrame', () => {
  it('serves the page its file by ranges', async () => {
    const source = bytesSource('a.pdf', new Uint8Array(100).map((_, index) => index))
    const { sent, fromPage } = renderFrame({ source, labels: { page: 'Page' } })
    fromPage({ type: 'transferly:ready' })
    expect(sent[0]).toMatchObject({ type: 'transferly:render', kind: 'pdf', size: 100, labels: { page: 'Page' } })

    fromPage({ type: 'transferly:range', id: 7, begin: 10, end: 13 })
    await waitFor(() => expect(sent[1]).toMatchObject({ type: 'transferly:bytes', id: 7 }))
    expect([...new Uint8Array(sent[1].bytes as ArrayBuffer)]).toEqual([10, 11, 12])
    fromPage({ type: 'transferly:range', id: 8, begin: 0, end: 5 * 1024 * 1024 })
    expect(sent[2]).toEqual({ type: 'transferly:bytes', id: 8, bytes: null })
  })

  it('answers null without a source or when the read fails', async () => {
    const bare = renderFrame()
    bare.fromPage({ type: 'transferly:ready' })
    expect(bare.sent[0]).toMatchObject({ size: 2, labels: {} })
    bare.fromPage({ type: 'transferly:range', id: 1, begin: 0, end: 1 })
    expect(bare.sent[1]).toEqual({ type: 'transferly:bytes', id: 1, bytes: null })
  })

  it('answers null when the read fails, forwards keys and reports failures', async () => {
    const { sent, frame, fromPage, onFailed } = renderFrame({ source: bytesSource('a.pdf', new Uint8Array(4), true) })
    fromPage({ type: 'transferly:range', id: 2, begin: 0, end: 2 })
    await waitFor(() => expect(sent[0]).toEqual({ type: 'transferly:bytes', id: 2, bytes: null }))

    const keys: string[] = []
    frame.addEventListener('keydown', (event) => keys.push(event.key))
    fromPage({ type: 'transferly:key', key: 'Escape' })
    expect(keys).toEqual(['Escape'])

    fromPage({ type: 'transferly:failed', reason: 'password' })
    fromPage({ type: 'transferly:failed' })
    expect(onFailed.mock.calls).toEqual([['password'], ['']])
  })
})
