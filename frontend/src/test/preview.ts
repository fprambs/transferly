import type { PreviewSource } from '@/features/preview/source'

/** A source over bytes in memory that records every range read, for the tests of the viewers. */
export function bytesSource(name: string, bytes: Uint8Array, fail = false): PreviewSource & { reads: Array<[number, number]> } {
  const reads: Array<[number, number]> = []
  return {
    key: `memory:${name}`,
    name,
    size: bytes.length,
    modifiedAt: '2026-09-29T12:00:00Z',
    reads,
    read: async (start, end) => {
      reads.push([start, end])
      if (fail) {
        throw new Error('unreadable')
      }
      return bytes.slice(start, Math.min(end, bytes.length))
    },
  }
}

/** A source over text in memory. */
export function textSource(name: string, text: string): PreviewSource & { reads: Array<[number, number]> } {
  return bytesSource(name, new TextEncoder().encode(text))
}
