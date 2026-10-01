import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { textSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { MarkdownViewer } from './MarkdownViewer'

/** Sends a message to the window as if the frame titled `title` had posted it. */
function fromFrame(title: string, data: unknown): void {
  const frame = screen.getByTitle(title) as HTMLIFrameElement
  window.dispatchEvent(new MessageEvent('message', { data, source: frame.contentWindow }))
}

/**
 * Covers {@link MarkdownViewer} and the {@link SandboxFrame} it renders in (ADR-0018).
 *
 * Scenarios:
 * - the file is rendered in a sandboxed frame of the isolated page, without same-origin, that gets the bytes and the theme once ready
 * - messages from other sources, and messages that are not of the page, are ignored
 * - a rendering that fails falls back to the source, which is also a click away
 */
describe('MarkdownViewer', () => {
  it('renders in the isolated page and falls back to the source', async () => {
    await renderWithProviders(<MarkdownViewer source={textSource('README.md', '# Hello')} kind="markdown" />)
    const frame = (await screen.findByTitle('Rendered README.md')) as HTMLIFrameElement
    expect(frame).toHaveAttribute('sandbox', 'allow-scripts')
    expect(frame).toHaveAttribute('src', '/sandbox/viewer.html')
    const sent: unknown[] = []
    ;(frame.contentWindow as Window).postMessage = (message: unknown) => void sent.push(message)

    window.dispatchEvent(new MessageEvent('message', { data: { type: 'transferly:ready' }, source: window }))
    act(() => fromFrame('Rendered README.md', { type: 'not-ours' }))
    expect(sent).toEqual([])
    act(() => fromFrame('Rendered README.md', { type: 'transferly:ready' }))
    expect(sent).toEqual([expect.objectContaining({ type: 'transferly:render', kind: 'markdown', name: 'README.md', theme: expect.any(Object) })])
    expect(new TextDecoder().decode((sent[0] as { bytes: ArrayBuffer }).bytes)).toBe('# Hello')

    act(() => fromFrame('Rendered README.md', { type: 'transferly:rendered' }))
    expect(screen.getByTitle('Rendered README.md')).toBeInTheDocument()
    act(() => fromFrame('Rendered README.md', { type: 'transferly:failed', reason: 'render' }))
    await waitFor(() => expect(screen.getByLabelText('Contents of README.md')).toHaveTextContent('# Hello'))

    await userEvent.setup().click(screen.getByRole('button', { name: 'Rendered' }))
    expect(await screen.findByTitle('Rendered README.md')).toBeInTheDocument()
  })
})
