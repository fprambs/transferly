import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { HexViewer } from './HexViewer'

/**
 * Covers {@link HexViewer} (ADR-0018).
 *
 * Scenarios:
 * - each row shows its offset, sixteen bytes in hexadecimal split in two groups, and the printable bytes as text with dots for the others
 * - the last row shows only the bytes left, and only the pages of the rows in view are read
 * - an empty file says so and reads nothing
 */
describe('HexViewer', () => {
  it('shows the bytes of the rows in view', async () => {
    const data = new Uint8Array(20_000)
    data.set(new TextEncoder().encode('Hello, bytes!\n'))
    const source = bytesSource('data.bin', data)
    await renderWithProviders(<HexViewer source={source} kind="binary" />)

    const table = screen.getByRole('table', { name: 'Bytes of data.bin' })
    const first = (await within(table).findAllByRole('row'))[0]
    expect(first).toHaveTextContent('00000000')
    expect(await within(first).findByText('48 65 6c 6c 6f 2c 20 62  79 74 65 73 21 0a 00 00', { normalizer: (text) => text })).toBeInTheDocument()
    expect(within(first).getByText('Hello, bytes!...')).toBeInTheDocument()
    expect(source.reads).toEqual([[0, 4096]])
  })

  it('shows the last bytes of a short file', async () => {
    const source = bytesSource('short.bin', new Uint8Array([0xca, 0xfe]))
    await renderWithProviders(<HexViewer source={source} kind="binary" />)
    expect(await screen.findByText(/^ca fe/)).toBeInTheDocument()
  })

  it('says a file is empty', async () => {
    const source = bytesSource('empty.bin', new Uint8Array())
    await renderWithProviders(<HexViewer source={source} kind="binary" />)
    expect(screen.getByText('This file is empty.')).toBeInTheDocument()
    expect(source.reads).toEqual([])
  })
})
