import type { ArchiveEntry, ArchiveListing } from '@/lib/api'
import { ApiError } from '@/lib/transport'

import { looksLikeBomb } from './viewers/archive-tree'
import { EntryTooLarge, entrySource, maxEntryBytes, readCapped, type ArchiveAccess, type PreviewSource } from './source'

/** The most entries a listing holds, as on the server. */
const maxEntries = 10_000

/** The caps of the streams read, in an object so the tests can lower them. */
export const localLimits = {
  /** The most bytes a stream is decompressed to while listing a TAR or looking for an entry, as on the server. */
  scanBytes: 1024 * 1024 * 1024,
}

/** The size of the chunks a file is read in. */
const chunkSize = 1024 * 1024

/** An archive of the computer in a format the browser does not read: 7z, RAR, bzip2, xz or zstd, which a server reads instead. */
export class LocalArchiveUnsupported extends Error {
  /** Name of the error. */
  override name = 'LocalArchiveUnsupported'
}

/** A failure explained by the guidance of a code of the server, since the browser meets the same cases the server does. */
function guided(code: 'archive_unsupported' | 'archive_encrypted' | 'archive_bomb' | 'archive_entry_not_found' | 'archive_too_large'): ApiError {
  return new ApiError({ type: 'about:blank', title: code, status: 422, code })
}

/** Reads a range of a blob. */
async function slice(blob: Blob, start: number, end: number): Promise<Uint8Array> {
  return new Uint8Array(await blob.slice(start, end).arrayBuffer())
}

/** A stream of the bytes of a blob, read in chunks as it is consumed. */
function streamOf(blob: Blob): ReadableStream<Uint8Array> {
  let at = 0
  return new ReadableStream({
    async pull(controller) {
      if (at >= blob.size) {
        controller.close()
        return
      }
      const end = Math.min(at + chunkSize, blob.size)
      controller.enqueue(await slice(blob, at, end))
      at = end
    },
  })
}

/** Decompresses gzip or raw deflate bytes with the decoder of the browser, failing past `limit` bytes. */
async function inflate(stream: ReadableStream<Uint8Array>, format: 'gzip' | 'deflate-raw', limit: number): Promise<Uint8Array> {
  try {
    return await readCapped(stream.pipeThrough(new DecompressionStream(format) as unknown as ReadableWritablePair<Uint8Array, Uint8Array>), limit)
  } catch (error) {
    throw error instanceof EntryTooLarge ? error : guided('archive_unsupported')
  }
}

/** Reads little-endian integers of a buffer. */
function view(bytes: Uint8Array): DataView {
  return new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
}

/** Turns a date and time of MS-DOS, as ZIP stores them, into an ISO instant. */
function dosTime(date: number, time: number): string | undefined {
  if (date === 0) {
    return undefined
  }
  return new Date(Date.UTC(1980 + (date >> 9), ((date >> 5) & 15) - 1, date & 31, time >> 11, (time >> 5) & 63, (time & 31) * 2)).toISOString()
}

/** Where a ZIP entry is and how it is stored. */
interface ZipEntry {
  /** The entry as the listing tells it. */
  entry: ArchiveEntry
  /** Where its local header starts. */
  offset: number
  /** Its compression method. */
  method: number
  /** Whether it is encrypted. */
  encrypted: boolean
}

/** Reads the central directory of a ZIP from the end of the file, ZIP64 included. */
async function readZip(blob: Blob): Promise<{ entries: ZipEntry[]; truncated: boolean }> {
  const tailStart = Math.max(0, blob.size - 65_557)
  const tail = await slice(blob, tailStart, blob.size)
  const tailView = view(tail)
  let end = -1
  for (let at = tail.length - 22; at >= 0 && end < 0; at--) {
    if (tailView.getUint32(at, true) === 0x06054b50) {
      end = at
    }
  }
  if (end < 0) {
    throw guided('archive_unsupported')
  }
  let count = tailView.getUint16(end + 10, true)
  let directorySize = tailView.getUint32(end + 12, true)
  let directoryOffset = tailView.getUint32(end + 16, true)
  if (end >= 20 && tailView.getUint32(end - 20, true) === 0x07064b50) {
    const zip64 = view(await slice(blob, Number(tailView.getBigUint64(end - 12, true)), blob.size))
    count = Number(zip64.getBigUint64(32, true))
    directorySize = Number(zip64.getBigUint64(40, true))
    directoryOffset = Number(zip64.getBigUint64(48, true))
  }
  const directory = await slice(blob, directoryOffset, directoryOffset + directorySize)
  const read = view(directory)
  const entries: ZipEntry[] = []
  let at = 0
  for (let index = 0; index < count && index < maxEntries; index++) {
    if (at + 46 > directory.length || read.getUint32(at, true) !== 0x02014b50) {
      throw guided('archive_unsupported')
    }
    const nameLength = read.getUint16(at + 28, true)
    const extraLength = read.getUint16(at + 30, true)
    const name = new TextDecoder().decode(directory.subarray(at + 46, at + 46 + nameLength))
    const values = { size: read.getUint32(at + 24, true), packed: read.getUint32(at + 20, true), offset: read.getUint32(at + 42, true) }
    for (let extra = at + 46 + nameLength; extra + 4 <= at + 46 + nameLength + extraLength; extra += 4 + read.getUint16(extra + 2, true)) {
      if (read.getUint16(extra, true) === 1) {
        let field = extra + 4
        for (const key of ['size', 'packed', 'offset'] as const) {
          if (values[key] === 0xffffffff) {
            values[key] = Number(read.getBigUint64(field, true))
            field += 8
          }
        }
      }
    }
    const kind = ((read.getUint32(at + 38, true) >>> 16) & 0o170000) === 0o120000 ? 'symlink' : name.endsWith('/') ? 'directory' : 'file'
    entries.push({
      entry: { name, type: kind, size: values.size, packed: values.packed, modified: dosTime(read.getUint16(at + 14, true), read.getUint16(at + 12, true)) },
      offset: values.offset,
      method: read.getUint16(at + 10, true),
      encrypted: (read.getUint16(at + 8, true) & 1) === 1,
    })
    at += 46 + nameLength + extraLength + read.getUint16(at + 32, true)
  }
  return { entries, truncated: count > maxEntries }
}

/** Reads the bytes of a ZIP entry, refusing encrypted entries, likely bombs and methods other than stored and deflated. */
async function readZipEntry(blob: Blob, found: ZipEntry): Promise<Uint8Array> {
  if (found.encrypted) {
    throw guided('archive_encrypted')
  }
  if (looksLikeBomb(found.entry)) {
    throw guided('archive_bomb')
  }
  const header = view(await slice(blob, found.offset, found.offset + 30))
  const start = found.offset + 30 + header.getUint16(26, true) + header.getUint16(28, true)
  const packed = blob.slice(start, start + found.entry.packed)
  if (found.method === 0) {
    return slice(packed, 0, packed.size)
  }
  if (found.method !== 8) {
    throw guided('archive_unsupported')
  }
  return inflate(streamOf(packed), 'deflate-raw', Math.min(maxEntryBytes, found.entry.size))
}

/** Reads a stream in exact amounts, for the blocks of a TAR. */
class BlockReader {
  /** The stream read. */
  private readonly reader: ReadableStreamDefaultReader<Uint8Array>
  /** Bytes read from the stream and not yet consumed. */
  private buffered = new Uint8Array()
  /** How many bytes were consumed. */
  consumed = 0

  /** Reads `stream`. */
  constructor(stream: ReadableStream<Uint8Array>) {
    this.reader = stream.getReader()
  }

  /** Reads exactly `length` bytes, or fewer when the stream ends first; fails past the scan cap. */
  async read(length: number): Promise<Uint8Array> {
    while (this.buffered.length < length) {
      const next = await this.reader.read()
      if (next.done) {
        break
      }
      const joined = new Uint8Array(this.buffered.length + next.value.length)
      joined.set(this.buffered)
      joined.set(next.value, this.buffered.length)
      this.buffered = joined
    }
    const taken = this.buffered.subarray(0, length)
    this.buffered = this.buffered.subarray(taken.length)
    this.consumed += taken.length
    if (this.consumed > localLimits.scanBytes) {
      throw guided('archive_too_large')
    }
    return taken
  }

  /** Stops reading the stream. */
  async close(): Promise<void> {
    await this.reader.cancel()
  }
}

/** Reads a text field of a TAR header, up to its first NUL. */
function field(block: Uint8Array, start: number, length: number): string {
  const bytes = block.subarray(start, start + length)
  const end = bytes.indexOf(0)
  return new TextDecoder().decode(end < 0 ? bytes : bytes.subarray(0, end))
}

/** Reads a number of a TAR header: octal text, or base 256 for large values. */
function number(block: Uint8Array, start: number, length: number): number {
  if ((block[start] & 0x80) !== 0) {
    return block.subarray(start + 1, start + length).reduce((value, byte) => value * 256 + byte, 0)
  }
  return Number.parseInt(field(block, start, length).trim() || '0', 8)
}

/** Walks the entries of a TAR stream, handing each one and a reader of its data to `visit` until it answers true; `visit` reads the data
 * only of the entry it stops at. */
async function walkTar(stream: ReadableStream<Uint8Array>, visit: (entry: ArchiveEntry, data: () => Promise<Uint8Array>) => Promise<boolean>): Promise<void> {
  const blocks = new BlockReader(stream)
  let longName: string | undefined
  try {
    for (;;) {
      const header = await blocks.read(512)
      if (header.length < 512 || header.every((byte) => byte === 0)) {
        return
      }
      if (field(header, 257, 5) !== 'ustar') {
        throw guided('archive_unsupported')
      }
      const size = number(header, 124, 12)
      const padded = Math.ceil(size / 512) * 512
      const flag = String.fromCharCode(header[156] || 0x30)
      if (flag === 'L' || flag === 'x') {
        const text = new TextDecoder().decode(await blocks.read(padded)).slice(0, size)
        longName = flag === 'L' ? text.replace(/\0+$/, '') : /(?:^|\n)\d+ path=([^\n]*)\n/.exec(text)?.[1] ?? longName
        continue
      }
      const prefix = field(header, 345, 155)
      const name = longName ?? (prefix === '' ? field(header, 0, 100) : `${prefix}/${field(header, 0, 100)}`)
      longName = undefined
      const type = flag === '5' ? 'directory' : flag === '2' || flag === '1' ? 'symlink' : flag === '0' || flag === '7' ? 'file' : undefined
      const data = async (): Promise<Uint8Array> => (await blocks.read(padded)).slice(0, size)
      if (type !== undefined && (await visit({ name, type, size, packed: -1, modified: new Date(number(header, 136, 12) * 1000).toISOString() }, data))) {
        return
      }
      await blocks.read(padded)
    }
  } finally {
    await blocks.close()
  }
}

/** The formats the browser reads. */
type LocalFormat = 'zip' | 'tar' | 'tar.gz' | 'gz'

/** Tells the format of an archive of the computer from its first bytes, and its name for a compressed TAR. */
async function detect(blob: Blob, name: string): Promise<LocalFormat> {
  const head = await slice(blob, 0, 512)
  if (head[0] === 0x50 && head[1] === 0x4b && (head[2] === 3 || head[2] === 5)) {
    return 'zip'
  }
  if (head[0] === 0x1f && head[1] === 0x8b) {
    return /\.(tgz|tar\.gz)$/i.test(name) ? 'tar.gz' : 'gz'
  }
  if (field(head, 257, 5) === 'ustar') {
    return 'tar'
  }
  throw new LocalArchiveUnsupported()
}

/** The name of the file inside a compressed single file: its own name without the compression suffix. */
function singleName(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(0, dot) : `${name}.out`
}

/**
 * The entries of an archive of the computer, read by the browser without sending it anywhere (D2.5): ZIP from its central directory and
 * the ranges of an entry, TAR and gzip-compressed TAR as streams, and a single gzip file, all decompressed by the browser, with the caps of
 * the server. Other formats fail with {@link LocalArchiveUnsupported}. An archive inside the archive is read the same way, from memory.
 * The file is opened once, on first use.
 */
export function localArchive(key: string, name: string, openBlob: () => Promise<Blob>): ArchiveAccess {
  let zip: Promise<ZipEntry[]> | undefined
  let opened: Promise<Blob> | undefined
  const open = (): Promise<Blob> => (opened ??= openBlob())
  const tarStream = async (format: LocalFormat): Promise<ReadableStream<Uint8Array>> => {
    const stream = streamOf(await open())
    return format === 'tar.gz' ? stream.pipeThrough(new DecompressionStream('gzip') as unknown as ReadableWritablePair<Uint8Array, Uint8Array>) : stream
  }
  const read = async (entry: ArchiveEntry): Promise<Uint8Array> => {
    const blob = await open()
    const format = await detect(blob, name)
    if (format === 'zip') {
      zip ??= readZip(blob).then((listing) => listing.entries)
      const found = (await zip).find((candidate) => candidate.entry.name === entry.name && candidate.entry.type === 'file')
      if (found === undefined) {
        throw guided('archive_entry_not_found')
      }
      return readZipEntry(blob, found)
    }
    if (format === 'gz') {
      return inflate(streamOf(blob), 'gzip', maxEntryBytes)
    }
    let bytes: Uint8Array | undefined
    await walkTar(await tarStream(format), async (candidate, data) => {
      if (candidate.name !== entry.name || candidate.type !== 'file') {
        return false
      }
      bytes = await data()
      return true
    }).catch((error: unknown) => {
      throw error instanceof ApiError ? error : guided('archive_unsupported')
    })
    if (bytes === undefined) {
      throw guided('archive_entry_not_found')
    }
    return bytes
  }
  return {
    list: async (): Promise<ArchiveListing> => {
      const blob = await open()
      const format = await detect(blob, name)
      if (format === 'zip') {
        const listing = await readZip(blob)
        zip = Promise.resolve(listing.entries)
        return { format, entries: listing.entries.map((found) => found.entry), truncated: listing.truncated }
      }
      if (format === 'gz') {
        return { format, entries: [{ name: singleName(name), type: 'file', size: -1, packed: blob.size }], truncated: false }
      }
      const entries: ArchiveEntry[] = []
      let truncated = false
      await walkTar(await tarStream(format), async (entry) => {
        truncated = entries.length >= maxEntries
        if (!truncated) {
          entries.push(entry)
        }
        return truncated
      }).catch((error: unknown) => {
        throw error instanceof ApiError ? error : guided('archive_unsupported')
      })
      return { format, entries, truncated }
    },
    open: async (entry) => {
      let bytes: Promise<Uint8Array> | undefined
      const load = (): Promise<Uint8Array> => (bytes ??= read(entry))
      const inner = `${key}!${entry.name}`
      return entrySource(inner, entry, load, localArchive(inner, entry.name, async () => new Blob([(await load()) as Uint8Array<ArrayBuffer>])))
    },
  }
}

/** A file of the computer whose entries the browser lists when it is an archive; `open` gives its contents, once. */
export function withLocalArchive(source: PreviewSource, open: () => Promise<Blob>): PreviewSource {
  return { ...source, archive: localArchive(source.key, source.name, open) }
}
