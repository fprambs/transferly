import { element, fill } from './office/common'

/** What the viewer tells of a font, read from its tables when it can. */
export interface FontInfo {
  /** The format of the file. */
  format: 'TrueType' | 'OpenType' | 'WOFF' | 'WOFF2'
  /** The names the font gives itself, by kind. */
  names: Partial<Record<'family' | 'style' | 'version' | 'copyright', string>>
  /** How many glyphs it has, when known. */
  glyphs?: number
  /** The code points it maps to glyphs, when known. */
  characters?: number[]
}

/** The most characters the grid shows. */
export const maxCharacters = 1000

/** The name identifiers of the name table the viewer shows. */
const nameIds: Record<number, keyof FontInfo['names']> = { 0: 'copyright', 1: 'family', 2: 'style', 5: 'version' }

/** Reads four bytes as a tag. */
function tag(view: DataView, at: number): string {
  return String.fromCharCode(view.getUint8(at), view.getUint8(at + 1), view.getUint8(at + 2), view.getUint8(at + 3))
}

/** Inflates a table of a WOFF file, which zlib compresses. */
async function inflate(bytes: Uint8Array): Promise<Uint8Array> {
  const stream = (new Response(bytes as Uint8Array<ArrayBuffer>).body as ReadableStream<Uint8Array>).pipeThrough(new DecompressionStream('deflate') as unknown as ReadableWritablePair<Uint8Array, Uint8Array>)
  return new Uint8Array(await new Response(stream).arrayBuffer())
}

/** Reads the tables of a TrueType, OpenType or WOFF file the viewer needs, by tag. */
async function readTables(bytes: Uint8Array, woff: boolean): Promise<Map<string, DataView>> {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const count = view.getUint16(woff ? 12 : 4)
  const tables = new Map<string, DataView>()
  for (let index = 0; index < count; index++) {
    const at = woff ? 44 + index * 20 : 12 + index * 16
    const name = tag(view, at)
    if (!['name', 'maxp', 'cmap'].includes(name)) {
      continue
    }
    const offset = view.getUint32(at + (woff ? 4 : 8))
    const length = view.getUint32(at + (woff ? 8 : 12))
    let table = bytes.subarray(offset, offset + length)
    if (woff && length < view.getUint32(at + 12)) {
      table = await inflate(table)
    }
    tables.set(name, new DataView(table.buffer, table.byteOffset, table.byteLength))
  }
  return tables
}

/** Reads the names of the name table: Windows names in UTF-16, or Macintosh ones in Latin-1. */
function readNames(table: DataView): FontInfo['names'] {
  const names: FontInfo['names'] = {}
  const count = table.getUint16(2)
  const strings = table.getUint16(4)
  for (let index = 0; index < count; index++) {
    const at = 6 + index * 12
    const platform = table.getUint16(at)
    const key = nameIds[table.getUint16(at + 6)]
    if (key === undefined || (platform !== 3 && platform !== 1) || (names[key] !== undefined && platform === 1)) {
      continue
    }
    const length = table.getUint16(at + 8)
    const offset = strings + table.getUint16(at + 10)
    const raw = new Uint8Array(table.buffer, table.byteOffset + offset, length)
    names[key] = new TextDecoder(platform === 3 ? 'utf-16be' : 'latin1').decode(raw)
  }
  return names
}

/** Reads the characters a cmap subtable of format 4 or 12 maps, at most {@link maxCharacters} printable ones. */
function readCharacters(table: DataView): number[] | undefined {
  const count = table.getUint16(2)
  const subtables = Array.from({ length: count }, (_, index) => ({
    platform: table.getUint16(4 + index * 8),
    encoding: table.getUint16(6 + index * 8),
    offset: table.getUint32(8 + index * 8),
  }))
  const chosen = subtables.find((sub) => sub.platform === 3 && sub.encoding === 10) ?? subtables.find((sub) => sub.platform === 3 && sub.encoding === 1)
  if (chosen === undefined) {
    return undefined
  }
  const ranges: Array<[number, number]> = []
  const format = table.getUint16(chosen.offset)
  if (format === 4) {
    const segments = table.getUint16(chosen.offset + 6) / 2
    for (let index = 0; index < segments; index++) {
      const end = table.getUint16(chosen.offset + 14 + index * 2)
      const start = table.getUint16(chosen.offset + 16 + segments * 2 + index * 2)
      ranges.push([start, end])
    }
  } else if (format === 12) {
    const groups = table.getUint32(chosen.offset + 12)
    for (let index = 0; index < groups; index++) {
      ranges.push([table.getUint32(chosen.offset + 16 + index * 12), table.getUint32(chosen.offset + 20 + index * 12)])
    }
  } else {
    return undefined
  }
  const characters: number[] = []
  for (const [start, end] of ranges) {
    for (let code = start; code <= end && characters.length < maxCharacters; code++) {
      if (code > 0x20 && code !== 0xffff && !(code >= 0x7f && code < 0xa0)) {
        characters.push(code)
      }
    }
  }
  return characters
}

/**
 * Reads what a font tells of itself: its format, and for TrueType, OpenType and WOFF files its names, the number of its glyphs and the
 * characters it maps. WOFF2, which Brotli compresses, is left to the browser, so only its format is told; a file whose tables cannot be read
 * tells only its format too.
 */
export async function readFont(bytes: Uint8Array): Promise<FontInfo> {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const signature = tag(view, 0)
  if (signature === 'wOF2') {
    return { format: 'WOFF2', names: {} }
  }
  const woff = signature === 'wOFF'
  const format = woff ? 'WOFF' : signature === 'OTTO' ? 'OpenType' : 'TrueType'
  try {
    const tables = await readTables(bytes, woff)
    const name = tables.get('name')
    const maxp = tables.get('maxp')
    const cmap = tables.get('cmap')
    return {
      format,
      names: name === undefined ? {} : readNames(name),
      glyphs: maxp?.getUint16(4),
      characters: cmap === undefined ? undefined : readCharacters(cmap),
    }
  } catch {
    return { format, names: {} }
  }
}

/** The characters shown when the font does not tell which it maps: Basic Latin and Latin-1. */
const defaultCharacters = [...Array.from({ length: 0x5e }, (_, index) => 0x21 + index), ...Array.from({ length: 0x5f }, (_, index) => 0xa1 + index)]

/** The sizes of the samples, in pixels. */
const sizes = [12, 16, 24, 36, 48, 72]

/** The family name the page gives the font it shows. */
const family = 'Transferly Preview Font'

/**
 * Shows a font inside the isolated page (ADR-0018): it is loaded with the FontFace interface of the browser, which checks it before using
 * it, and shown in samples of several sizes, with a text of the person's own, and in a grid of the characters it maps. What the font tells
 * of itself (format, family, style, version, copyright and glyphs) heads the page. A file the browser refuses as a font fails.
 */
export async function renderFont(bytes: Uint8Array, root: HTMLElement, labels: Record<string, string>): Promise<void> {
  const face = new FontFace(family, bytes.slice().buffer)
  await face.load()
  document.fonts.add(face)
  const info = await readFont(bytes)
  const fontFamily = `'${family}', sans-serif`

  root.classList.add('font')
  const header = element('header', 'font-header')
  header.append(element('h1', 'font-name', [info.names.family, info.names.style].filter(Boolean).join(' ') || labels.unnamed))
  const facts = element('dl', 'font-facts')
  const fact = (label: string, value: string | undefined): void => {
    if (value !== undefined && value !== '') {
      facts.append(element('dt', undefined, label), element('dd', undefined, value))
    }
  }
  fact(labels.format, info.format)
  fact(labels.version, info.names.version)
  fact(labels.glyphs, info.glyphs === undefined ? undefined : String(info.glyphs))
  fact(labels.copyright, info.names.copyright)
  header.append(facts)

  const input = element('input', 'pdf-search font-input')
  input.value = labels.sample
  input.setAttribute('aria-label', labels.try)
  const samples = element('div', 'font-samples')
  const drawSamples = (): void => {
    samples.replaceChildren(
      ...sizes.map((size) => {
        const line = element('p', 'font-sample', input.value)
        line.style.fontFamily = fontFamily
        line.style.fontSize = `${size}px`
        line.dataset.size = `${size}px`
        return line
      }),
    )
  }
  input.addEventListener('input', drawSamples)
  drawSamples()

  const characters = info.characters ?? defaultCharacters
  const grid = element('div', 'font-grid')
  grid.setAttribute('aria-label', labels.characters)
  for (const code of characters) {
    const cell = element('span', 'font-char', String.fromCodePoint(code))
    cell.style.fontFamily = fontFamily
    cell.title = `U+${code.toString(16).toUpperCase().padStart(4, '0')}`
    grid.append(cell)
  }
  const gridTitle = element('h2', 'font-section', fill(info.characters === undefined ? labels.charactersUnknown : labels.charactersKnown, { count: characters.length }))
  root.append(header, input, samples, gridTitle, grid)
}
