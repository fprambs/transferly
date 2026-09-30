import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { IsolatedViewer } from './IsolatedViewer'

/** Sends a message to the window as if the frame titled `title` had posted it. */
function fromFrame(title: string, data: unknown): void {
  const frame = screen.getByTitle(title) as HTMLIFrameElement
  act(() => window.dispatchEvent(new MessageEvent('message', { data, source: frame.contentWindow })))
}

/**
 * Covers {@link IsolatedViewer} (D2.6, D2.7).
 *
 * Scenarios:
 * - the whole file is read and opens in the isolated page as its kind, with the words of the interface
 * - a document protected with a password says so, and a document or database that cannot be read says so; all offer their bytes
 */
describe('IsolatedViewer', () => {
  it('opens in the isolated page', async () => {
    const source = bytesSource('book.xlsx', new Uint8Array([0x50, 0x4b, 3, 4, 1, 2]))
    await renderWithProviders(<IsolatedViewer source={source} kind="spreadsheet" />)
    const frame = (await screen.findByTitle('Document book.xlsx')) as HTMLIFrameElement
    expect(source.reads).toEqual([[0, 6]])
    const sent: Array<Record<string, unknown>> = []
    ;(frame.contentWindow as Window).postMessage = (message: Record<string, unknown>) => void sent.push(message)
    fromFrame('Document book.xlsx', { type: 'sentinel:ready' })
    expect(sent[0]).toMatchObject({
      kind: 'spreadsheet',
      labels: { sheets: 'Sheets', rowsCut: 'Showing the first {{shown}} of {{total}} rows.', slides: 'Slides: {{total}}', slide: 'Slide {{number}}', rows: 'Rows {{from}}–{{to}} of {{total}}', resultRows: '{{count}} rows' },
    })
  })

  it('explains protected and broken documents', async () => {
    await renderWithProviders(
      <>
        <IsolatedViewer source={bytesSource('locked.docx', new Uint8Array(8))} kind="document" />
        <IsolatedViewer source={bytesSource('bad.pptx', new Uint8Array(8))} kind="presentation" />
        <IsolatedViewer source={bytesSource('bad.sqlite', new Uint8Array(8))} kind="sqlite" />
      </>,
    )
    await screen.findByTitle('Document locked.docx')
    fromFrame('Document locked.docx', { type: 'sentinel:failed', reason: 'password' })
    fromFrame('Document bad.pptx', { type: 'sentinel:failed', reason: 'render' })
    fromFrame('Database bad.sqlite', { type: 'sentinel:failed', reason: 'render' })
    expect(await screen.findByText('This document is protected with a password')).toBeInTheDocument()
    expect(screen.getByText('This document cannot be opened')).toBeInTheDocument()
    expect(screen.getByText('This database cannot be opened')).toBeInTheDocument()
    await userEvent.setup().click(screen.getAllByRole('button', { name: 'Show as bytes' })[0])
    await waitFor(() => expect(screen.getByRole('table', { name: 'Bytes of locked.docx' })).toBeInTheDocument())
  })
})
