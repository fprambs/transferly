import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import type { ArchiveEntry } from '@/lib/api'
import { ApiError } from '@/lib/transport'

import { LocalArchiveUnsupported, localArchive, localLimits, withLocalArchive } from './local-archive'
import { EntryTooLarge, type ArchiveAccess, type PreviewSource } from './source'
import { bytesSource } from '@/test/preview'

/** Reads a file of the preview samples, shared with the test stack; the tests run from the frontend folder. */
function sample(path: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples', path)))
}

/** An archive of the computer over bytes. */
function archiveOf(name: string, bytes: Uint8Array): ArchiveAccess {
  return localArchive(`local:${name}`, name, async () => new Blob([bytes as Uint8Array<ArrayBuffer>]))
}

/** Opens an entry of an archive and reads it whole. */
async function readEntry(archive: ArchiveAccess, entry: ArchiveEntry): Promise<Uint8Array> {
  const source = await archive.open(entry)
  return source.read(0, source.size)
}

/** The entry of a listing with the given name. */
async function entryNamed(archive: ArchiveAccess, name: string): Promise<ArchiveEntry> {
  return (await archive.list()).entries.find((entry) => entry.name === name) as ArchiveEntry
}

/** Expects a guided failure with the given code. */
async function expectCode(promise: Promise<unknown>, code: string): Promise<void> {
  await expect(promise).rejects.toSatisfy((error) => error instanceof ApiError && error.code === code)
}

/** Compresses bytes with raw deflate, as ZIP stores them. */
async function deflate(bytes: Uint8Array): Promise<Uint8Array> {
  const stream = new Response(bytes as Uint8Array<ArrayBuffer>).body as ReadableStream<Uint8Array>
  return new Uint8Array(await new Response(stream.pipeThrough(new CompressionStream('deflate-raw') as unknown as ReadableWritablePair<Uint8Array, Uint8Array>)).arrayBuffer())
}

/** A file of a ZIP the tests write by hand. */
interface ZipFile {
  /** Its name. */
  name: string
  /** The bytes stored for it. */
  data: Uint8Array
  /** The size it declares once extracted; the length of the data by default. */
  size?: number
  /** Compression method; stored by default. */
  method?: number
  /** General purpose flags. */
  flags?: number
  /** External attributes. */
  attributes?: number
  /** MS-DOS date; a day of 2026 by default. */
  date?: number
  /** Writes its sizes and offset as ZIP64 values. */
  zip64?: boolean
  /** An extra field of another kind before the ZIP64 one. */
  otherExtra?: boolean
  /** Keeps the offset out of the ZIP64 values. */
  keepOffset?: boolean
}

/** Joins byte arrays. */
function concat(parts: readonly Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0))
  let at = 0
  for (const part of parts) {
    out.set(part, at)
    at += part.length
  }
  return out
}

/** Writes little-endian fields. */
function fields(...values: Array<[number | bigint, 2 | 4 | 8]>): Uint8Array {
  const out = new Uint8Array(values.reduce((sum, [, width]) => sum + width, 0))
  const data = new DataView(out.buffer)
  let at = 0
  for (const [value, width] of values) {
    if (width === 2) data.setUint16(at, Number(value), true)
    else if (width === 4) data.setUint32(at, Number(value), true)
    else data.setBigUint64(at, BigInt(value), true)
    at += width
  }
  return out
}

/** Writes a ZIP by hand, with the flags, methods and ZIP64 records real archivers write, and a ZIP64 end when asked. */
function zipOf(files: readonly ZipFile[], zip64End = false): Uint8Array {
  const locals: Uint8Array[] = []
  const centrals: Uint8Array[] = []
  let offset = 0
  for (const file of files) {
    const name = new TextEncoder().encode(file.name)
    const size = file.size ?? file.data.length
    const local = concat([fields([0x04034b50, 4], [20, 2], [file.flags ?? 0, 2], [file.method ?? 0, 2], [0, 2], [file.date ?? 0x5b3e, 2], [0, 4], [file.data.length, 4], [size, 4], [name.length, 2], [0, 2]), name, file.data])
    const big = file.zip64 === true
    const extra = concat([
      file.otherExtra === true ? fields([0x5455, 2], [1, 2]) : new Uint8Array(),
      file.otherExtra === true ? new Uint8Array([0]) : new Uint8Array(),
      big ? fields([1, 2], [file.keepOffset === true ? 16 : 24, 2], [size, 8], [file.data.length, 8]) : new Uint8Array(),
      big && file.keepOffset !== true ? fields([offset, 8]) : new Uint8Array(),
    ])
    centrals.push(
      concat([
        fields([0x02014b50, 4], [20, 2], [20, 2], [file.flags ?? 0, 2], [file.method ?? 0, 2], [0, 2], [file.date ?? 0x5b3e, 2], [0, 4],
          [big ? 0xffffffff : file.data.length, 4], [big ? 0xffffffff : size, 4], [name.length, 2], [extra.length, 2], [0, 2], [0, 2], [0, 2],
          [file.attributes ?? 0, 4], [big && file.keepOffset !== true ? 0xffffffff : offset, 4]),
        name,
        extra,
      ]),
    )
    locals.push(local)
    offset += local.length
  }
  const directory = concat(centrals)
  const tail: Uint8Array[] = []
  if (zip64End) {
    const end64 = offset + directory.length
    tail.push(fields([0x06064b50, 4], [44, 8], [45, 2], [45, 2], [0, 4], [0, 4], [files.length, 8], [files.length, 8], [directory.length, 8], [offset, 8]))
    tail.push(fields([0x07064b50, 4], [0, 4], [end64, 8], [1, 4]))
  }
  tail.push(fields([0x06054b50, 4], [0, 2], [0, 2], [zip64End ? 0xffff : files.length, 2], [zip64End ? 0xffff : files.length, 2],
    [zip64End ? 0xffffffff : directory.length, 4], [zip64End ? 0xffffffff : offset, 4], [0, 2]))
  return concat([...locals, directory, ...tail])
}

/** A header block of a TAR, with the magic of ustar unless told otherwise. */
function tarHeader(name: string, size: number, flag: string, options: { bigSize?: boolean; magic?: string; prefix?: string; noTime?: boolean } = {}): Uint8Array {
  const block = new Uint8Array(512)
  const put = (text: string, at: number) => block.set(new TextEncoder().encode(text), at)
  put(name, 0)
  put('0000644\0', 100)
  if (options.bigSize === true) {
    block[124] = 0x80
    new DataView(block.buffer).setUint32(131 - 3 + 4, size)
  } else {
    put(`${size.toString(8).padStart(11, '0')}\0`, 124)
  }
  put(options.noTime === true ? '' : '15252330000\0', 136)
  block[156] = flag === '' ? 0 : flag.charCodeAt(0)
  put(options.magic ?? 'ustar\0', 257)
  put(options.prefix ?? '', 345)
  return block
}

/** The data of a TAR entry, padded to its blocks. */
function tarData(text: string): Uint8Array {
  const bytes = new TextEncoder().encode(text)
  const out = new Uint8Array(Math.ceil(bytes.length / 512) * 512)
  out.set(bytes)
  return out
}

afterEach(() => {
  localLimits.scanBytes = 1024 * 1024 * 1024
})

/**
 * Covers the archives the browser reads from the computer (D2.5).
 *
 * Scenarios:
 * - the ZIP, TAR, gzip TAR and single gzip samples list the tree the server lists, and their files read as the originals; an archive
 *   nested in a ZIP is listed and read from memory
 * - a listing stops at 10 000 entries and says so; a ZIP cut in half, a damaged central directory and damaged gzip data are refused;
 *   7z and other formats are left to the server
 * - encrypted ZIP entries, likely bombs, unknown methods and missing entries are refused with the guidance of the server's codes; an entry
 *   larger than it declares is too large
 * - ZIP64 sizes, offsets and end records, extra fields of other kinds, links and entries with no date are read
 * - TAR long names of GNU and pax, prefixes, sizes in base 256, entries with no type flag, special files, archives with no end blocks and
 *   headers without the ustar magic are handled; decompressing past the scan cap fails
 * - a file of the local panel gets the archive of the browser, which opens the file once
 */
describe('local archives', () => {
  it('reads the samples', async () => {
    const zip = archiveOf('project.zip', sample('archives/project.zip'))
    const listing = await zip.list()
    expect(listing).toMatchObject({ format: 'zip', truncated: false })
    expect(listing.entries).toHaveLength(13)
    expect(await readEntry(zip, await entryNamed(zip, 'project/README.md'))).toEqual(sample('markdown/README.md'))
    expect(await readEntry(zip, await entryNamed(zip, 'project/images/sample.png'))).toEqual(sample('images/sample.png'))
    const inner = (await zip.open(await entryNamed(zip, 'project/vendor/inner.zip'))).archive as ArchiveAccess
    expect(await readEntry(inner, await entryNamed(inner, 'notes/plain.txt'))).toEqual(sample('text/plain.txt'))

    for (const name of ['project.tar', 'project.tar.gz']) {
      const tar = archiveOf(name, sample(`archives/${name}`))
      const entries = (await tar.list()).entries
      expect(entries.find((entry) => entry.name === 'project/LATEST')?.type).toBe('symlink')
      expect(entries.find((entry) => entry.name === 'project/src/')?.type).toBe('directory')
      expect(await readEntry(tar, await entryNamed(tar, 'project/src/code.go'))).toEqual(sample('text/code.go'))
      await expectCode(tar.open({ name: 'none', type: 'file', size: 1, packed: -1 }), 'archive_entry_not_found')
    }

    const log = archiveOf('server.log.gz', sample('archives/server.log.gz'))
    expect((await log.list()).entries).toEqual([{ name: 'server.log', type: 'file', size: -1, packed: sample('archives/server.log.gz').length }])
    expect(await readEntry(log, { name: 'server.log', type: 'file', size: -1, packed: 1 })).toEqual(sample('text/server.log'))
    expect((await archiveOf('noext', new Uint8Array([0x1f, 0x8b])).list()).entries[0].name).toBe('noext.out')
  })

  it('refuses what it cannot or must not read', async () => {
    const many = await archiveOf('many.zip', sample('archives/many-entries.zip')).list()
    expect(many.truncated).toBe(true)
    expect(many.entries).toHaveLength(10_000)
    await expectCode(archiveOf('t.zip', sample('archives/truncated.zip')).list(), 'archive_unsupported')
    await expect(archiveOf('p.7z', sample('archives/project.7z')).list()).rejects.toBeInstanceOf(LocalArchiveUnsupported)
    const secret = archiveOf('secret.zip', sample('archives/secret.zip'))
    await expectCode(secret.open(await entryNamed(secret, 'project/README.md')), 'archive_encrypted')
    const bomb = archiveOf('bomb.zip', sample('archives/bomb.zip'))
    await expect(bomb.open(await entryNamed(bomb, 'zeros.bin'))).rejects.toBeInstanceOf(EntryTooLarge)
    await expectCode(archiveOf('bad.gz', new Uint8Array([0x1f, 0x8b, 1, 2, 3])).open({ name: 'bad', type: 'file', size: -1, packed: 5 }), 'archive_unsupported')
    await expectCode(archiveOf('bad.tgz', new Uint8Array([0x1f, 0x8b, 1, 2, 3])).list(), 'archive_unsupported')
    await expectCode(archiveOf('bad.tgz', new Uint8Array([0x1f, 0x8b, 1, 2, 3])).open({ name: 'x', type: 'file', size: 1, packed: 1 }), 'archive_unsupported')
  })

  it('reads hand-written ZIPs', async () => {
    const zeros = new Uint8Array(2 * 1024 * 1024)
    const text = new TextEncoder().encode('hello, zip')
    const archive = archiveOf(
      'hand.zip',
      zipOf(
        [
          { name: 'bomb.bin', data: await deflate(zeros), size: zeros.length, method: 8 },
          { name: 'odd.bin', data: text, method: 12 },
          { name: 'big64.txt', data: await deflate(text), size: text.length, method: 8, zip64: true, otherExtra: true },
          { name: 'link', data: text, attributes: 0o120777 << 16, date: 0 },
          { name: 'small64.txt', data: text, zip64: true, keepOffset: true },
          { name: 'liar.txt', data: await deflate(text), size: 3, method: 8 },
          { name: 'broken.txt', data: new Uint8Array([0xff, 0xff, 0xff]), size: 10, method: 8 },
        ],
        true,
      ),
    )
    const entries = (await archive.list()).entries
    expect(entries.map((entry) => entry.type)).toEqual(['file', 'file', 'file', 'symlink', 'file', 'file', 'file'])
    expect(entries[0].modified).toBe('2025-09-30T00:00:00.000Z')
    expect(entries[3].modified).toBeUndefined()
    await expectCode(archive.open(entries[0]), 'archive_bomb')
    await expectCode(archive.open(entries[1]), 'archive_unsupported')
    expect(new TextDecoder().decode(await readEntry(archive, entries[2]))).toBe('hello, zip')
    await expectCode(archive.open(entries[3]), 'archive_entry_not_found')
    expect(new TextDecoder().decode(await readEntry(archive, entries[4]))).toBe('hello, zip')
    await expect(archive.open(entries[5])).rejects.toBeInstanceOf(EntryTooLarge)
    await expectCode(archive.open(entries[6]), 'archive_unsupported')
    const unlisted = archiveOf('hand.zip', zipOf([{ name: 'a.txt', data: text }]))
    expect(new TextDecoder().decode(await readEntry(unlisted, { name: 'a.txt', type: 'file', size: text.length, packed: text.length }))).toBe('hello, zip')
    expect(await archiveOf('empty.zip', zipOf([])).list()).toEqual({ format: 'zip', entries: [], truncated: false })

    const damaged = zipOf([{ name: 'a', data: text }])
    damaged[text.length + 31] = 0
    await expectCode(archiveOf('damaged.zip', damaged).list(), 'archive_unsupported')
  })

  it('reads hand-written TARs', async () => {
    const longName = 'deep/'.repeat(30) + 'file.txt'
    const pax = `${String(`30 path=pax/named.txt\n`.length + 3).padStart(2, '0')} path=pax/named.txt\n`
    const tar = concat([
      tarHeader('././@LongLink', longName.length + 1, 'L'),
      tarData(`${longName}\0`),
      tarHeader('ignored', 5, '0'),
      tarData('long!'),
      tarHeader('PaxHeader', pax.length, 'x'),
      tarData(pax),
      tarHeader('short', 3, ''),
      tarData('pax'),
      tarHeader('PaxHeader', 11, 'x'),
      tarData('11 a=b\nxyz'),
      tarHeader('dev', 0, '3'),
      tarHeader('big.bin', 4, '0', { bigSize: true }),
      tarData('four'),
      tarHeader('name.txt', 2, '0', { prefix: 'pre', noTime: true }),
      tarData('ok'),
    ])
    const archive = archiveOf('hand.tar', tar)
    const entries = (await archive.list()).entries
    expect(entries.map((entry) => [entry.name, entry.size])).toEqual([
      [longName, 5],
      ['pax/named.txt', 3],
      ['big.bin', 4],
      ['pre/name.txt', 2],
    ])
    expect(new TextDecoder().decode(await readEntry(archive, entries[3]))).toBe('ok')
    const bad = archiveOf('bad.tar', concat([tarHeader('a', 0, '0'), tarHeader('b', 0, '0', { magic: 'nope\0' })]))
    await expectCode(bad.list(), 'archive_unsupported')
    await expectCode(bad.open({ name: 'b', type: 'file', size: 0, packed: -1 }), 'archive_unsupported')
    expect(entries[3].modified).toBe('1970-01-01T00:00:00.000Z')
    const crowded = await archiveOf('many.tar', concat(Array.from({ length: 10_001 }, (_, index) => tarHeader(`f${index}`, 0, '0')))).list()
    expect(crowded.truncated).toBe(true)
    expect(crowded.entries).toHaveLength(10_000)

    localLimits.scanBytes = 1024
    await expectCode(archive.list(), 'archive_too_large')
  })

  it('gives files of the local panel their archive', async () => {
    let opened = 0
    const bytes = sample('archives/project.zip')
    const source: PreviewSource = withLocalArchive(bytesSource('project.zip', bytes), async () => (opened++, new Blob([bytes as Uint8Array<ArrayBuffer>])))
    const archive = source.archive as ArchiveAccess
    await archive.list()
    await readEntry(archive, await entryNamed(archive, 'project/src/code.go'))
    expect(opened).toBe(1)
  })
})
