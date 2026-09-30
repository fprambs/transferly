import { describe, expect, it } from 'vitest'

import { detectKind } from './detect'

/** Bytes of text. */
function text(value: string): Uint8Array {
  return new TextEncoder().encode(value)
}

/** Bytes that start with `prefix` and go on with zeros. */
function bytes(...prefix: number[]): Uint8Array {
  const out = new Uint8Array(600)
  out.set(prefix)
  return out
}

/** A TAR header: its magic at offset 257. */
function tar(): Uint8Array {
  const out = new Uint8Array(600)
  out.set(text('ustar'), 257)
  return out
}

/**
 * Covers {@link detectKind} (ADR-0018).
 *
 * Scenarios:
 * - signatures decide whatever the extension says: PDF, images, audio, video, archives, SQLite and fonts
 * - a ZIP named as an Office document is that document, an OLE file named .xls is a spreadsheet, and other OLE files are binary
 * - text keeps the text kind of its extension, or is plain text; a PEM block is a certificate whatever its name
 * - binary bytes without a signature keep the binary kind of their extension, or are binary; a file that lies about being text is binary
 * - truncated UTF-8 at the end of the head still reads as text, while NUL bytes and invalid UTF-8 do not
 */
describe('detectKind', () => {
  it('trusts the signatures', () => {
    expect(detectKind('report.txt', text('%PDF-1.7'))).toBe('pdf')
    expect(detectKind('x', bytes(0x89, 0x50, 0x4e, 0x47))).toBe('image')
    expect(detectKind('x', bytes(0xff, 0xd8, 0xff))).toBe('image')
    expect(detectKind('x', text('RIFF1234WEBPVP8'))).toBe('image')
    expect(detectKind('x', text('RIFF1234WAVEfmt'))).toBe('audio')
    expect(detectKind('x', text('RIFF1234AVI LIST'))).toBe('video')
    expect(detectKind('x', text('ID3\u0004'))).toBe('audio')
    expect(detectKind('x', bytes(0, 0, 0, 0x20, ...text('ftypM4A')))).toBe('audio')
    expect(detectKind('x', bytes(0, 0, 0, 0x20, ...text('ftypisom')))).toBe('video')
    expect(detectKind('x', bytes(0, 0, 0, 0x20, ...text('ftypavif')))).toBe('image')
    expect(detectKind('x', bytes(0x1a, 0x45, 0xdf, 0xa3))).toBe('video')
    expect(detectKind('x', bytes(0x50, 0x4b, 3, 4))).toBe('archive')
    expect(detectKind('x', bytes(0x1f, 0x8b))).toBe('archive')
    expect(detectKind('x', bytes(0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c))).toBe('archive')
    expect(detectKind('x', tar())).toBe('archive')
    expect(detectKind('x', text('SQLite format 3\u0000'))).toBe('sqlite')
    expect(detectKind('x', text('wOF2'))).toBe('font')
  })

  it('refines shared containers by the extension', () => {
    expect(detectKind('Plan.DOCX', bytes(0x50, 0x4b, 3, 4))).toBe('document')
    expect(detectKind('book.xlsx', bytes(0x50, 0x4b, 3, 4))).toBe('spreadsheet')
    expect(detectKind('slides.pptx', bytes(0x50, 0x4b, 3, 4))).toBe('presentation')
    expect(detectKind('app.jar', bytes(0x50, 0x4b, 3, 4))).toBe('archive')
    expect(detectKind('old.xls', bytes(0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1))).toBe('spreadsheet')
    expect(detectKind('old.doc', bytes(0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1))).toBe('binary')
  })

  it('reads text by its extension', () => {
    expect(detectKind('README.md', text('# Title'))).toBe('markdown')
    expect(detectKind('data.json', text('{"a":1}'))).toBe('json')
    expect(detectKind('ci.yml', text('a: 1'))).toBe('yaml')
    expect(detectKind('feed.xml', text('<rss/>'))).toBe('xml')
    expect(detectKind('rows.tsv', text('a\tb'))).toBe('csv')
    expect(detectKind('logo.svg', text('<svg/>'))).toBe('svg')
    expect(detectKind('mail.eml', text('From: a'))).toBe('email')
    expect(detectKind('notes', text('hello'))).toBe('text')
    expect(detectKind('main.go', text('package main'))).toBe('text')
    expect(detectKind('site.key', text('-----BEGIN CERTIFICATE-----'))).toBe('certificate')
    expect(detectKind('photo.png', text('not really an image'))).toBe('text')
    expect(detectKind('empty.txt', new Uint8Array())).toBe('text')
  })

  it('keeps binary kinds by the extension', () => {
    expect(detectKind('cut.png', bytes(0x13, 0x37))).toBe('image')
    expect(detectKind('cert.der', bytes(0x30, 0x82))).toBe('certificate')
    expect(detectKind('font.ttf', bytes(0, 1, 0, 0, 0))).toBe('font')
    expect(detectKind('blob.bin', bytes(0x13, 0x37))).toBe('binary')
    expect(detectKind('lies.txt', bytes(0x13, 0x37))).toBe('binary')
  })

  it('tells text from other bytes', () => {
    expect(detectKind('cut.txt', text('añ').subarray(0, 2))).toBe('text')
    expect(detectKind('bad.txt', new Uint8Array([0xc3, 0x28]))).toBe('binary')
  })
})
