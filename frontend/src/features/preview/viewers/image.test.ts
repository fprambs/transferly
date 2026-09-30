import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

import { decodeTiff, imageType, isTiff, TooManyPixels } from './image'

/** A sample image of the test stack. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(`../testenv/preview-samples/images/${name}`))
}

/** A little-endian TIFF header that declares a single page of `width` by `height` pixels and nothing else. */
function header(width: number, height: number): Uint8Array {
  const view = new DataView(new ArrayBuffer(8 + 2 + 2 * 12 + 4))
  view.setUint8(0, 0x49)
  view.setUint8(1, 0x49)
  view.setUint16(2, 42, true)
  view.setUint32(4, 8, true)
  view.setUint16(8, 2, true)
  const entry = (at: number, tag: number, value: number): void => {
    view.setUint16(at, tag, true)
    view.setUint16(at + 2, 4, true)
    view.setUint32(at + 4, 1, true)
    view.setUint32(at + 8, value, true)
  }
  entry(10, 256, width)
  entry(22, 257, height)
  return new Uint8Array(view.buffer)
}

/**
 * Covers the helpers of the image viewer (ADR-0018).
 *
 * Scenarios:
 * - TIFF is told by both byte orders, and nothing else is taken for it
 * - a TIFF decodes into its pixels, four bytes each
 * - a header that claims more than 100 megapixels is refused before decoding; one missing its height is not taken for large
 * - SVG is shown with its media type and other formats are left to the browser
 */
describe('image helpers', () => {
  it('tells and decodes TIFF', () => {
    expect(isTiff(sample('sample.tiff'))).toBe(true)
    expect(isTiff(new Uint8Array([0x4d, 0x4d, 0x00, 0x2a]))).toBe(true)
    expect(isTiff(sample('sample.png'))).toBe(false)
    const pixels = decodeTiff(sample('sample.tiff'))
    expect([pixels.width, pixels.height, pixels.rgba.length]).toEqual([640, 400, 640 * 400 * 4])
  })

  it('refuses a TIFF that claims too many pixels', () => {
    expect(() => decodeTiff(header(20_000, 20_000))).toThrow(TooManyPixels)
    const noHeight = header(20_000, 20_000)
    new DataView(noHeight.buffer).setUint16(8, 1, true)
    expect(() => decodeTiff(noHeight)).not.toThrow(TooManyPixels)
  })

  it('names the media type of SVG only', () => {
    expect(imageType('svg')).toBe('image/svg+xml')
    expect(imageType('image')).toBe('')
  })
})
