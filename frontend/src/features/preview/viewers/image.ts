import { decode, decodeImage, toRGBA8 } from 'utif'

/** The largest image file read, in bytes. */
export const imageLimit = 50 * 1024 * 1024

/** The most pixels a TIFF may have to be decoded; a larger header is refused before any pixel is allocated. */
export const maxTiffPixels = 100_000_000

/** Tells whether bytes are a TIFF file, which browsers do not draw. */
export function isTiff(bytes: Uint8Array): boolean {
  const [a, b, c, d] = bytes
  return (a === 0x49 && b === 0x49 && c === 0x2a && d === 0x00) || (a === 0x4d && b === 0x4d && c === 0x00 && d === 0x2a)
}

/** The pixels of a decoded TIFF. */
export interface Pixels {
  /** Width in pixels. */
  width: number
  /** Height in pixels. */
  height: number
  /** The pixels, four bytes each: red, green, blue and alpha. */
  rgba: Uint8ClampedArray<ArrayBuffer>
}

/** The error of a TIFF too large to decode. */
export class TooManyPixels extends Error {}

/** Reads the first value of a tag of a TIFF directory. */
function tag(ifd: Record<string, unknown>, name: string): number {
  const value = ifd[name] as number[] | undefined
  return value?.[0] ?? 0
}

/**
 * Decodes the first page of a TIFF file into pixels with UTIF.js. The size its header declares is checked before decoding, so a file that
 * claims more than {@link maxTiffPixels} is refused with {@link TooManyPixels} without allocating them.
 */
export function decodeTiff(bytes: Uint8Array): Pixels {
  const buffer = bytes.slice().buffer
  const page = decode(buffer)[0]
  if (tag(page, 't256') * tag(page, 't257') > maxTiffPixels) {
    throw new TooManyPixels()
  }
  decodeImage(buffer, page)
  const rgba = new Uint8ClampedArray(toRGBA8(page))
  return { width: page.width, height: page.height, rgba }
}

/** The media type an image is shown with: SVG needs its own for the browser to draw it; other formats are recognized by their bytes. */
export function imageType(kind: string): string {
  return kind === 'svg' ? 'image/svg+xml' : ''
}
