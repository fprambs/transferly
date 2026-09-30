import { beforeEach, describe, expect, it, vi } from 'vitest'

import { readPhotoInfo } from './exif'

/** What the mocked exifr answers next. */
const parse = vi.hoisted(() => vi.fn())

vi.mock('exifr/dist/lite.esm.mjs', () => ({ default: { parse } }))

/**
 * Covers how {@link readPhotoInfo} writes what exifr reads (ADR-0018).
 *
 * Scenarios:
 * - exposure, aperture, focal length and location are written as a photographer reads them
 * - long exposures are written in seconds, and a camera with only a maker keeps it
 * - a file without metadata, or one exifr cannot read, gives nothing
 */
describe('readPhotoInfo formats', () => {
  beforeEach(() => parse.mockReset())

  it('writes the values', async () => {
    parse.mockResolvedValueOnce({ ExposureTime: 0.004, FNumber: 2.8, FocalLength: 35, latitude: -33.45, longitude: -70.66667 })
    expect(await readPhotoInfo(new Uint8Array(4))).toEqual({ exposure: '1/250 s', aperture: 'f/2.8', focal: '35 mm', location: '-33.45000, -70.66667' })
    parse.mockResolvedValueOnce({ ExposureTime: 2, Make: 'Only' })
    expect(await readPhotoInfo(new Uint8Array(4))).toEqual({ exposure: '2 s', camera: 'Only' })
  })

  it('gives nothing without metadata', async () => {
    parse.mockResolvedValueOnce(undefined)
    expect(await readPhotoInfo(new Uint8Array(4))).toEqual({})
    parse.mockRejectedValueOnce(new Error('unknown file format'))
    expect(await readPhotoInfo(new Uint8Array(4))).toEqual({})
  })
})
