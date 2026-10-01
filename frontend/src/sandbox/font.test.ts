import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { maxCharacters, readFont, renderFont } from './font'

/** The words of the interface, as the parent hands them. */
const labels = {
  unnamed: 'Unnamed',
  format: 'Format',
  version: 'Version',
  glyphs: 'Glyphs',
  copyright: 'Copyright',
  sample: 'Sample text',
  try: 'Try',
  characters: 'Characters',
  charactersKnown: 'Has {{count}}',
  charactersUnknown: 'Latin',
}

/** Reads a font sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/fonts', name)))
}

/** Writes big-endian fields. */
function fields(...values: Array<[number, 2 | 4]>): number[] {
  const out: number[] = []
  for (const [value, width] of values) {
    for (let shift = (width - 1) * 8; shift >= 0; shift -= 8) {
      out.push((value >>> shift) & 0xff)
    }
  }
  return out
}

/** Writes a TrueType file by hand with the given tables, by tag. */
function sfnt(tables: Record<string, number[]>, signature = 0x00010000): Uint8Array {
  const tags = Object.keys(tables)
  const out = [...fields([signature, 4], [tags.length, 2], [0, 2], [0, 2], [0, 2])]
  let offset = 12 + tags.length * 16
  const bodies: number[] = []
  for (const tagName of tags) {
    out.push(...[...tagName].map((char) => char.charCodeAt(0)), ...fields([0, 4], [offset, 4], [tables[tagName].length, 4]))
    bodies.push(...tables[tagName])
    offset += tables[tagName].length
  }
  return new Uint8Array([...out, ...bodies])
}

/** A name table with one Macintosh record, one Windows record of the same name, and one of another platform. */
function nameTable(): number[] {
  const mac = [...'MacFamily'].map((char) => char.charCodeAt(0))
  const windows = [...'WinFamily'].flatMap((char) => [0, char.charCodeAt(0)])
  const records = [
    fields([1, 2], [0, 2], [0, 2], [1, 2], [mac.length, 2], [0, 2]),
    fields([3, 2], [1, 2], [0x409, 2], [1, 2], [windows.length, 2], [mac.length, 2]),
    fields([1, 2], [0, 2], [0, 2], [1, 2], [mac.length, 2], [0, 2]),
    fields([0, 2], [3, 2], [0, 2], [2, 2], [mac.length, 2], [0, 2]),
    fields([3, 2], [1, 2], [0x409, 2], [9, 2], [mac.length, 2], [0, 2]),
  ].flat()
  return [...fields([0, 2], [5, 2], [6 + 5 * 12, 2]), ...records, ...mac, ...windows]
}

/** A cmap table with one subtable of the given platform, encoding and body. */
function cmapTable(platform: number, encoding: number, body: number[]): number[] {
  return [...fields([0, 2], [1, 2], [platform, 2], [encoding, 2], [12, 4]), ...body]
}

/** A cmap subtable of format 12 mapping the given ranges. */
function format12(ranges: Array<[number, number]>): number[] {
  return [...fields([12, 2], [0, 2], [16 + ranges.length * 12, 4], [0, 4], [ranges.length, 4]), ...ranges.flatMap(([start, end]) => fields([start, 4], [end, 4], [0, 4]))]
}

let root: HTMLElement
let loaded: Array<{ family: string; source: ArrayBuffer }>

beforeEach(() => {
  root = document.createElement('div')
  loaded = []
  vi.stubGlobal(
    'FontFace',
    class {
      family: string
      source: ArrayBuffer
      constructor(family: string, source: ArrayBuffer) {
        this.family = family
        this.source = source
      }
      async load(): Promise<void> {
        if (new Uint8Array(this.source).length < 300) {
          throw new DOMException('invalid font', 'SyntaxError')
        }
        loaded.push(this)
      }
    },
  )
  Object.defineProperty(document, 'fonts', { configurable: true, value: { add: vi.fn() } })
})

/**
 * Covers {@link readFont} and {@link renderFont} (D2.7).
 *
 * Scenarios:
 * - a TrueType and a WOFF font tell their names, glyphs and the characters they map; WOFF2 tells only its format
 * - OpenType is told by its signature, Windows names win over Macintosh ones, other platforms and names are ignored, cmap subtables of
 *   format 12 are read, other formats and fonts without a Windows subtable tell no characters, the grid stops at its cap, and a font
 *   whose tables cannot be read tells only its format
 * - a font is loaded with FontFace and shown with its facts, samples of several sizes that follow the text typed, and the grid of its
 *   characters, or of Latin characters when the format does not tell; a font the browser refuses fails
 */
describe('fonts', () => {
  it('reads what the samples tell of themselves', async () => {
    const ttf = await readFont(sample('blocks.ttf'))
    expect(ttf).toMatchObject({ format: 'TrueType', names: { family: 'Transferly Blocks', style: 'Regular', version: 'Version 1.0' }, glyphs: 10 })
    expect(ttf.characters?.map((code) => String.fromCharCode(code)).join('')).toBe('AEILNOSTaeilnost')
    expect(await readFont(sample('blocks.woff'))).toMatchObject({ format: 'WOFF', names: { family: 'Transferly Blocks' }, glyphs: 10 })
    expect(await readFont(sample('blocks.woff2'))).toEqual({ format: 'WOFF2', names: {} })
  })

  it('reads the edges of the tables', async () => {
    expect(await readFont(sfnt({ name: nameTable() }, 0x4f54544f))).toMatchObject({ format: 'OpenType', names: { family: 'WinFamily' } })
    const wide = await readFont(sfnt({ cmap: cmapTable(3, 10, format12([[0x20, 0x22], [0x7f, 0x80], [0x1f600, 0x1f601]])) }))
    expect(wide.characters).toEqual([0x21, 0x22, 0x1f600, 0x1f601])
    const many = await readFont(sfnt({ cmap: cmapTable(3, 10, format12([[0x100, 0x10000]])) }))
    expect(many.characters).toHaveLength(maxCharacters)
    expect((await readFont(sfnt({ cmap: cmapTable(3, 1, fields([6, 2], [0, 2])) }))).characters).toBeUndefined()
    expect((await readFont(sfnt({ cmap: cmapTable(1, 0, fields([0, 2])) }))).characters).toBeUndefined()
    expect(await readFont(sfnt({ glyf: [1, 2, 3] }))).toEqual({ format: 'TrueType', names: {}, glyphs: undefined, characters: undefined })
    expect(await readFont(sample('truncated.ttf'))).toEqual({ format: 'TrueType', names: {} })
  })

  it('shows a font', async () => {
    await renderFont(sample('blocks.ttf'), root, labels)
    expect(loaded[0].family).toBe('Transferly Preview Font')
    expect(document.fonts.add).toHaveBeenCalledWith(loaded[0])
    expect(root.querySelector('h1')?.textContent).toBe('Transferly Blocks Regular')
    expect(Array.from(root.querySelectorAll('dt'), (node) => node.textContent)).toEqual(['Format', 'Version', 'Glyphs', 'Copyright'])
    const samples = root.querySelectorAll<HTMLElement>('.font-sample')
    expect(Array.from(samples, (node) => node.style.fontSize)).toEqual(['12px', '16px', '24px', '36px', '48px', '72px'])
    expect(samples[0].style.fontFamily).toContain('Transferly Preview Font')
    const input = root.querySelector('input') as HTMLInputElement
    input.value = 'transferly'
    input.dispatchEvent(new Event('input'))
    expect(root.querySelector('.font-sample')?.textContent).toBe('transferly')
    expect(root.querySelector('.font-section')?.textContent).toBe('Has 16')
    expect(root.querySelector('.font-char')?.getAttribute('title')).toBe('U+0041')

    const woff2 = document.createElement('div')
    await renderFont(sample('blocks.woff2'), woff2, labels)
    expect(woff2.querySelector('h1')?.textContent).toBe('Unnamed')
    expect(woff2.querySelector('.font-section')?.textContent).toBe('Latin')
    expect(woff2.querySelectorAll('.font-char')).toHaveLength(0x5e + 0x5f)
    expect(Array.from(woff2.querySelectorAll('dt'), (node) => node.textContent)).toEqual(['Format'])

    await expect(renderFont(sample('truncated.ttf'), document.createElement('div'), labels)).rejects.toThrow('invalid font')
  })
})
