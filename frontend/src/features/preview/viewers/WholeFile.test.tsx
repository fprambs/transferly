import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { WholeFile } from './WholeFile'

/**
 * Covers {@link WholeFile} (ADR-0018).
 *
 * Scenarios:
 * - a file within the limit is read whole once and handed to the viewer
 * - a file over the limit is not read, says so with the limit, and can be shown as bytes
 * - a failed read says so and can be tried again
 */
describe('WholeFile', () => {
  it('reads a file within the limit', async () => {
    const source = bytesSource('a.txt', new TextEncoder().encode('hello'))
    await renderWithProviders(<WholeFile source={source} limit={10}>{(bytes) => <p>{bytes.length} bytes read</p>}</WholeFile>)
    expect(await screen.findByText('5 bytes read')).toBeInTheDocument()
    expect(source.reads).toEqual([[0, 5]])
  })

  it('offers the bytes of a file over the limit', async () => {
    const source = bytesSource('big.txt', new Uint8Array(20))
    await renderWithProviders(<WholeFile source={source} limit={10}>{() => <p>never</p>}</WholeFile>)
    expect(screen.getByText('Too large to show here')).toBeInTheDocument()
    expect(screen.getByText(/up to 10 B/)).toBeInTheDocument()
    expect(source.reads).toEqual([])
    await userEvent.setup().click(screen.getByRole('button', { name: 'Show as bytes' }))
    expect(await screen.findByRole('table', { name: 'Bytes of big.txt' })).toBeInTheDocument()
  })

  it('tries a failed read again', async () => {
    const source = bytesSource('broken.txt', new Uint8Array(4), true)
    await renderWithProviders(<WholeFile source={source} limit={10}>{() => <p>never</p>}</WholeFile>)
    expect(await screen.findByRole('alert')).toHaveTextContent('The file could not be read.')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }))
    await screen.findByRole('alert')
    expect(source.reads).toHaveLength(2)
  })
})
