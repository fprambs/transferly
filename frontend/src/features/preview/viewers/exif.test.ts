import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

import { readPhotoInfo } from './exif'

/**
 * Covers {@link readPhotoInfo} (ADR-0018).
 *
 * Scenarios:
 * - the camera, time, sensitivity and software of a photo are read from its EXIF
 */
describe('readPhotoInfo', () => {
  it('reads the metadata of a photo', async () => {
    const info = await readPhotoInfo(new Uint8Array(readFileSync('../testenv/preview-samples/images/sample.jpg')))
    expect(info).toMatchObject({ camera: 'Transferly Camera Preview One', iso: '200', software: 'Transferly samples' })
    expect(info.taken).toMatch(/^2026-09-29T/)
  })
})
