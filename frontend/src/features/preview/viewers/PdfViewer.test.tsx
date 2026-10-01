import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { PdfViewer } from './PdfViewer'

/** Sends a message to the window as if the frame titled `title` had posted it. */
function fromFrame(title: string, data: unknown): void {
  const frame = screen.getByTitle(title) as HTMLIFrameElement
  act(() => window.dispatchEvent(new MessageEvent('message', { data, source: frame.contentWindow })))
}

/**
 * Covers {@link PdfViewer} (ADR-0018).
 *
 * Scenarios:
 * - the first MiB is read and the document opens in the isolated page with the words of the interface
 * - a document that cannot be opened says so and offers its bytes
 */
describe('PdfViewer', () => {
  it('opens in the isolated page', async () => {
    const source = bytesSource('doc.pdf', new TextEncoder().encode('%PDF-1.4 ...'))
    await renderWithProviders(<PdfViewer source={source} kind="pdf" />)
    const frame = (await screen.findByTitle('Document doc.pdf')) as HTMLIFrameElement
    expect(source.reads).toEqual([[0, 1024 * 1024]])
    const sent: Array<Record<string, unknown>> = []
    ;(frame.contentWindow as Window).postMessage = (message: Record<string, unknown>) => void sent.push(message)
    fromFrame('Document doc.pdf', { type: 'transferly:ready' })
    expect(sent[0]).toMatchObject({ kind: 'pdf', labels: { thumbnails: 'Pages', position: 'Page {{page}} of {{total}}', page: 'Page {{number}}', open: 'Open', password: 'Password' } })
  })

  it('explains broken documents', async () => {
    await renderWithProviders(<PdfViewer source={bytesSource('bad.pdf', new Uint8Array(8))} kind="pdf" />)
    await screen.findByTitle('Document bad.pdf')
    fromFrame('Document bad.pdf', { type: 'transferly:failed', reason: 'render' })
    expect(await screen.findByText('This PDF cannot be opened')).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Show as bytes' }))
    await waitFor(() => expect(screen.getByRole('table', { name: 'Bytes of bad.pdf' })).toBeInTheDocument())
  })
})
