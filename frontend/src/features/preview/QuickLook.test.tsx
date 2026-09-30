import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { PreviewSource } from './source'
import { bytesSource, textSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { QuickLook } from './QuickLook'

/** Holds the index of the viewer the way a caller does, and records what it was asked. */
function Harness({ sources, onDownload, onEdit, start = 0 }: { sources: PreviewSource[]; onDownload?: (source: PreviewSource) => void; onEdit?: (source: PreviewSource) => void; start?: number }) {
  const [index, setIndex] = useState<number | null>(start)
  return (
    <>
      <p>open: {index === null ? 'none' : String(index)}</p>
      <QuickLook sources={sources} index={index} onIndexChange={setIndex} onClose={() => setIndex(null)} onDownload={onDownload} onEdit={onEdit} />
    </>
  )
}

/** Three files of a folder: text, binary and an unreadable one. */
function folder() {
  return [textSource('notes.txt', 'plain words'), bytesSource('data.bin', new Uint8Array([0, 1, 2, 3])), bytesSource('broken.pdf', new Uint8Array(4), true)]
}

/**
 * Covers {@link QuickLook} (ADR-0018).
 *
 * Scenarios:
 * - it opens full screen on the file asked for, naming it with its size and its place in the folder, and tells its kind from its bytes
 * - text opens in its viewer; a kind without a viewer yet is shown as bytes and says so; a binary file is shown as bytes without the note
 * - the arrows and the buttons move to the previous and next file and stop at the ends; the arrows with a modifier and other keys do nothing
 * - download and, for text, open in the editor call their actions; without them the buttons are left out
 * - a file that cannot be read says so and can be read again, and an empty file says so without a viewer
 * - Escape and the close button close it
 */
describe('QuickLook', () => {
  it('shows a file and walks the folder', async () => {
    const onDownload = vi.fn()
    const onEdit = vi.fn()
    const user = userEvent.setup()
    await renderWithProviders(<Harness sources={folder()} onDownload={onDownload} onEdit={onEdit} />)

    expect(await screen.findByRole('dialog', { name: 'notes.txt' })).toHaveTextContent('11 B · 1 of 3')
    expect(await screen.findByText('Text')).toBeInTheDocument()
    expect(screen.queryByText(/no viewer of this kind of file exists yet/)).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Open in the editor' }))
    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ name: 'notes.txt' }))
    await user.click(screen.getByRole('button', { name: 'Download' }))
    expect(onDownload).toHaveBeenCalledWith(expect.objectContaining({ name: 'notes.txt' }))
    expect(screen.getByRole('button', { name: 'Previous file' })).toBeDisabled()

    await user.keyboard('{ArrowRight}')
    expect(await screen.findByRole('dialog', { name: 'data.bin' })).toHaveTextContent('2 of 3')
    expect(await screen.findByText('Binary file')).toBeInTheDocument()
    expect(screen.queryByText(/no viewer of this kind/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open in the editor' })).not.toBeInTheDocument()
    await user.keyboard('{Control>}{ArrowRight}{/Control}')
    await user.keyboard('x')
    expect(screen.getByRole('dialog', { name: 'data.bin' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Next file' }))
    expect(await screen.findByRole('dialog', { name: 'broken.pdf' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('The file could not be read.')
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    expect(screen.getByRole('button', { name: 'Next file' })).toBeDisabled()
    await user.keyboard('{ArrowRight}')
    await user.keyboard('{ArrowLeft}')
    expect(await screen.findByRole('dialog', { name: 'data.bin' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Previous file' }))
    expect(await screen.findByRole('dialog', { name: 'notes.txt' })).toBeInTheDocument()

    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.getByText('open: none')).toBeInTheDocument())
  })

  it('says a file is empty without choosing a viewer', async () => {
    await renderWithProviders(<Harness sources={[textSource('empty.txt', '')]} />)
    expect(await screen.findByText('This file is empty.')).toBeInTheDocument()
    expect(screen.queryByText('Text')).not.toBeInTheDocument()
  })

  it('shows a kind without a viewer yet as bytes, and says so', async () => {
    await renderWithProviders(<Harness sources={[bytesSource('doc.pdf', new TextEncoder().encode('%PDF-1.7'))]} />)
    expect(await screen.findByText('PDF document')).toBeInTheDocument()
    expect(screen.getByText(/no viewer of this kind of file exists yet/)).toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'Bytes of doc.pdf' })).toBeInTheDocument()
  })

  it('leaves out the actions it was not given, and closes from its button', async () => {
    const user = userEvent.setup()
    await renderWithProviders(<Harness sources={folder()} />)
    await screen.findByText('Text')
    expect(screen.queryByRole('button', { name: 'Download' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open in the editor' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.getByText('open: none')).toBeInTheDocument())
  })
})
