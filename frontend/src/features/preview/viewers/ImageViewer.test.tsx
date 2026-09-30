import { readFileSync } from 'node:fs'

import { act, cleanup, fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { ImageViewer } from './ImageViewer'

/** A sample image of the test stack as a source. */
function sample(name: string) {
  return bytesSource(name, new Uint8Array(readFileSync(`../testenv/preview-samples/images/${name}`)))
}

/** Tells the image that it loaded, with the size a browser would find. */
function load(image: HTMLElement, width: number, height: number): void {
  Object.defineProperty(image, 'naturalWidth', { value: width })
  Object.defineProperty(image, 'naturalHeight', { value: height })
  fireEvent.load(image)
}

/** A TIFF header that claims 20000 × 20000 pixels, more than the viewer decodes. */
function hugeTiff(): Uint8Array {
  const view = new DataView(new ArrayBuffer(38))
  view.setUint16(0, 0x4949)
  view.setUint16(2, 42, true)
  view.setUint32(4, 8, true)
  view.setUint16(8, 2, true)
  for (const [at, tag] of [[10, 256], [22, 257]]) {
    view.setUint16(at, tag, true)
    view.setUint16(at + 2, 4, true)
    view.setUint32(at + 4, 1, true)
    view.setUint32(at + 8, 20_000, true)
  }
  return new Uint8Array(view.buffer)
}

/**
 * Covers {@link ImageViewer} (ADR-0018).
 *
 * Scenarios:
 * - an image is drawn from a blob URL, revoked when the viewer closes, and tells its size once loaded
 * - zoom steps in and out, fits and shows the real size; rotation turns both ways; the keys + - 0 and R do the same, and keys with a
 *   modifier do nothing
 * - the info panel shows the size and the camera metadata, or says there is none
 * - SVG gets its media type; TIFF is decoded onto a canvas with its size
 * - an image the browser cannot decode, and a TIFF too large to decode, say so and offer their bytes
 */
describe('ImageViewer', () => {
  beforeEach(() => {
    URL.createObjectURL = vi.fn(() => 'blob:image')
    URL.revokeObjectURL = vi.fn()
  })

  it('shows, zooms and rotates an image', async () => {
    await renderWithProviders(<ImageViewer source={sample('sample.jpg')} kind="image" />)
    const image = await screen.findByRole('img', { name: 'sample.jpg' })
    expect(image).toHaveAttribute('src', 'blob:image')
    load(image, 640, 400)
    expect(screen.getAllByText('640 × 400 px')[0]).toBeInTheDocument()

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Zoom in' }))
    expect(screen.getByText('150%')).toBeInTheDocument()
    expect(image.style.width).toBe('960px')
    await user.click(screen.getByRole('button', { name: 'Zoom out' }))
    await user.click(screen.getByRole('button', { name: 'Zoom out' }))
    expect(screen.getByText('75%')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '100%' }))
    expect(screen.getByRole('button', { name: '100%' })).toHaveAttribute('aria-pressed', 'true')
    await user.click(screen.getByRole('button', { name: 'Rotate right' }))
    expect(image.style.transform).toBe('rotate(90deg)')
    await user.click(screen.getByRole('button', { name: 'Rotate left' }))
    await user.click(screen.getByRole('button', { name: 'Rotate left' }))
    expect(image.style.transform).toBe('rotate(270deg)')

    await user.keyboard('r')
    expect(image.style.transform).toBe('rotate(0deg)')
    await user.keyboard('+')
    expect(screen.getByText('150%')).toBeInTheDocument()
    await user.keyboard('=')
    await user.keyboard('-')
    expect(screen.getByText('150%')).toBeInTheDocument()
    await user.keyboard('{Control>}-{/Control}')
    expect(screen.getByText('150%')).toBeInTheDocument()
    for (let step = 0; step < 12; step++) {
      await user.keyboard('+')
    }
    expect(screen.getByText('800%')).toBeInTheDocument()
    await user.keyboard('0')
    expect(screen.getByRole('button', { name: 'Fit' })).toHaveAttribute('aria-pressed', 'true')
    await user.keyboard('x')
    await user.click(screen.getByRole('button', { name: '100%' }))
    await user.click(screen.getByRole('button', { name: 'Fit' }))
    expect(screen.getByRole('button', { name: 'Fit' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: 'Info' }))
    const info = await screen.findByRole('complementary', { name: 'Info' })
    expect(info).toHaveTextContent('Sentinel Camera Preview One')
    expect(info).toHaveTextContent('Sep 29, 2026')
    cleanup()
    await vi.waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:image'), { timeout: 3000 })
  })

  it('draws SVG with its type and TIFF on a canvas, and says when there is no metadata', async () => {
    const created: Blob[] = []
    URL.createObjectURL = vi.fn((blob: Blob) => (created.push(blob), 'blob:svg'))
    await renderWithProviders(
      <>
        <ImageViewer source={sample('logo.svg')} kind="svg" />
        <ImageViewer source={sample('sample.tiff')} kind="image" />
      </>,
    )
    await screen.findByRole('img', { name: 'logo.svg' })
    expect(created[0].type).toBe('image/svg+xml')
    expect(await screen.findByRole('img', { name: 'sample.tiff' })).toBeInstanceOf(HTMLCanvasElement)
    expect(screen.getAllByText('640 × 400 px')[0]).toBeInTheDocument()
    await userEvent.setup().click(screen.getAllByRole('button', { name: 'Info' })[0])
    expect(await screen.findByText('No camera metadata in this file.')).toBeInTheDocument()
    expect(screen.getByRole('complementary', { name: 'Info' })).toHaveTextContent('Dimensions—')
  })

  it('offers the bytes of an image it cannot decode', async () => {
    await renderWithProviders(
      <>
        <ImageViewer source={sample('truncated.png')} kind="image" />
        <ImageViewer source={bytesSource('huge.tif', hugeTiff())} kind="image" />
      </>,
    )
    const truncated = await screen.findByRole('img', { name: 'truncated.png' })
    act(() => fireEvent.error(truncated))
    expect(await screen.findAllByText('This image cannot be shown')).toHaveLength(2)
    await userEvent.setup().click(screen.getAllByRole('button', { name: 'Show as bytes' })[0])
    expect(await screen.findByRole('table', { name: 'Bytes of truncated.png' })).toBeInTheDocument()
  })
})
