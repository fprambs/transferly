/** Work budgets applied before handing an Office package to a renderer. */
export interface OfficeBudgets {
  /** Most bytes in one decoded part. */
  entryBytes: number
  /** Most decoded bytes in the whole package. */
  totalBytes: number
  /** Most parts in the package. */
  entries: number
  /** Most bytes of central directory metadata. */
  directoryBytes: number
  /** Largest accepted compression ratio for large parts. */
  ratio: number
}

/** Default limits; tests may supply smaller independent budgets without allocating hostile sizes. */
export const officeBudgets: OfficeBudgets = { entryBytes: 8 * 1024 * 1024, totalBytes: 64 * 1024 * 1024, entries: 1000, directoryBytes: 4 * 1024 * 1024, ratio: 200 }

/** Bounds a compressed Office package before a third-party renderer materializes its parts. */
export async function checkOfficeBudget(bytes: Uint8Array, budget: OfficeBudgets = officeBudgets): Promise<void> {
  if (bytes.length < 22) throw new Error('invalid Office package')
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  let end = -1
  for (let at = bytes.length - 22; at >= Math.max(0, bytes.length - 65557); at--) {
    if (view.getUint32(at, true) === 0x06054b50 && at + 22 + view.getUint16(at + 20, true) === bytes.length) { end = at; break }
  }
  if (end < 0) throw new Error('invalid Office directory')
  const count = view.getUint16(end + 10, true)
  const size = view.getUint32(end + 12, true)
  let at = view.getUint32(end + 16, true)
  if (count > budget.entries || size > budget.directoryBytes || at + size > end) throw new Error('Office directory exceeds limits')
  let total = 0
  for (let index = 0; index < count; index++) {
    if (at + 46 > end || view.getUint32(at, true) !== 0x02014b50) throw new Error('invalid Office entry')
    const packed = view.getUint32(at + 20, true)
    const unpacked = view.getUint32(at + 24, true)
    total += unpacked
    if (unpacked > budget.entryBytes || total > budget.totalBytes || (unpacked > 1024 * 1024 && unpacked / Math.max(packed, 1) > budget.ratio)) throw new Error('Office part exceeds limits')
    const method = view.getUint16(at + 10, true)
    const local = view.getUint32(at + 42, true)
    if (local + 30 > end || view.getUint32(local, true) !== 0x04034b50) throw new Error('invalid Office local header')
    const begin = local + 30 + view.getUint16(local + 26, true) + view.getUint16(local + 28, true)
    if (begin + packed > end || (method !== 0 && method !== 8)) throw new Error('invalid Office compression')
    const payload = bytes.slice(begin, begin + packed)
    const compressed = new ReadableStream<Uint8Array>({ start(controller) { controller.enqueue(payload); controller.close() } })
    const stream = method === 0 ? compressed : compressed.pipeThrough(new DecompressionStream('deflate-raw') as unknown as ReadableWritablePair<Uint8Array, Uint8Array>)
    const reader = stream.getReader()
    let actual = 0
    try {
      for (let chunk = await reader.read(); !chunk.done; chunk = await reader.read()) {
        actual += chunk.value.length
        if (actual > unpacked) throw new Error('Office decompression exceeds its declared size')
      }
      if (actual !== unpacked) throw new Error('Office decompression size mismatch')
    } finally {
      try { await reader.cancel() } finally { reader.releaseLock() }
    }
    at += 46 + view.getUint16(at + 28, true) + view.getUint16(at + 30, true) + view.getUint16(at + 32, true)
  }
  if (at > end) throw new Error('invalid Office bounds')
}
