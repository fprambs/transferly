import JSZip from 'jszip'
import { describe, expect, it } from 'vitest'

import { checkOfficeBudget, officeBudgets } from './budget'

/** Builds a small valid Office-shaped package without requiring an Office renderer. */
async function packageOf(content: string | Uint8Array = 'abc', compression: 'STORE' | 'DEFLATE' = 'STORE'): Promise<Uint8Array> {
  const zip = new JSZip()
  zip.file('content.xml', content)
  return zip.generateAsync({ type: 'uint8array', compression })
}

/** Mutates metadata of a bounded package to model hostile declarations independently from its small payload. */
function changed(bytes: Uint8Array, change: (view: DataView, end: number, directory: number) => void): Uint8Array {
  const copy = bytes.slice()
  const view = new DataView(copy.buffer, copy.byteOffset, copy.byteLength)
  const end = copy.length - 22
  change(view, end, view.getUint32(end + 16, true))
  return copy
}

/** Validates directory, allocation, compression and actual expansion limits before a library can materialize the document. */
describe('Office admission budgets', () => {
  it('accepts ordinary stored and compressed parts and empty packages', async () => {
    await expect(checkOfficeBudget(await packageOf())).resolves.toBeUndefined()
    await expect(checkOfficeBudget(await packageOf('abc', 'DEFLATE'))).resolves.toBeUndefined()
    await expect(checkOfficeBudget(await new JSZip().generateAsync({ type: 'uint8array' }))).resolves.toBeUndefined()
    const random = new Uint8Array(1024 * 1024 + 1)
    for (let offset = 0; offset < random.length; offset += 65536) crypto.getRandomValues(random.subarray(offset, Math.min(random.length, offset + 65536)))
    await expect(checkOfficeBudget(await packageOf(random, 'DEFLATE'))).resolves.toBeUndefined()
  })

  it('rejects truncated, oversized and out-of-bounds directories before allocating their metadata', async () => {
    const bytes = await packageOf()
    await expect(checkOfficeBudget(new Uint8Array())).rejects.toThrow()
    for (const mutate of [
      (v: DataView, end: number) => v.setUint32(end, 0),
      (v: DataView, end: number) => v.setUint16(end + 10, 1001, true),
      (v: DataView, end: number) => v.setUint32(end + 12, 4 * 1024 * 1024 + 1, true),
      (v: DataView, end: number) => v.setUint32(end + 16, end + 1, true),
      (v: DataView, _end: number, directory: number) => v.setUint32(directory, 0),
      (v: DataView, end: number) => { v.setUint32(end + 12, 0, true); v.setUint32(end + 16, end - 1, true) },
    ]) await expect(checkOfficeBudget(changed(bytes, mutate))).rejects.toThrow()
  })

  it('rejects oversized parts, suspicious ratios and aggregate expansion', async () => {
    const bytes = await packageOf()
    await expect(checkOfficeBudget(changed(bytes, (v, _end, directory) => v.setUint32(directory + 24, 8 * 1024 * 1024 + 1, true)))).rejects.toThrow()
    await expect(checkOfficeBudget(changed(bytes, (v, _end, directory) => v.setUint32(directory + 24, 1024 * 1024 + 1, true)))).rejects.toThrow()
    await expect(checkOfficeBudget(bytes, { ...officeBudgets, totalBytes: 2 })).rejects.toThrow()
  })

  it('checks local bounds, methods and actual decoded sizes including forged small declarations', async () => {
    const bytes = await packageOf()
    for (const mutate of [
      (v: DataView, _end: number, directory: number) => v.setUint32(v.getUint32(directory + 42, true), 0),
      (v: DataView, end: number, directory: number) => v.setUint32(directory + 42, end - 20, true),
      (v: DataView, end: number, directory: number) => v.setUint32(directory + 20, end, true),
      (v: DataView, _end: number, directory: number) => v.setUint16(directory + 10, 9, true),
      (v: DataView, _end: number, directory: number) => v.setUint32(directory + 24, 2, true),
      (v: DataView, _end: number, directory: number) => v.setUint32(directory + 24, 4, true),
      (v: DataView, _end: number, directory: number) => v.setUint16(directory + 28, 65535, true),
    ]) await expect(checkOfficeBudget(changed(bytes, mutate))).rejects.toThrow()
  })
})
