import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import JSZip from 'jszip'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { fixBullets, renderDocument } from './document'

/** Reads an Office sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/office', name)))
}

/** The namespaces an OpenDocument written by hand declares. */
const declarations = [
  'xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"',
  'xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"',
  'xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"',
  'xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0"',
  'xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0"',
  'xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0"',
  'xmlns:xlink="http://www.w3.org/1999/xlink"',
].join(' ')

/** Writes an OpenDocument text by hand, with the given styles, body and files. */
async function odt(styles: string, body: string, files: Record<string, string> = {}): Promise<Uint8Array> {
  const zip = new JSZip()
  zip.file('mimetype', 'application/vnd.oasis.opendocument.text')
  zip.file('content.xml', `<office:document-content ${declarations}><office:automatic-styles>${styles}</office:automatic-styles><office:body>${body}</office:body></office:document-content>`)
  for (const [name, content] of Object.entries(files)) {
    zip.file(name, content)
  }
  return zip.generateAsync({ type: 'uint8array' })
}

let root: HTMLElement

beforeEach(() => {
  root = document.createElement('div')
  let made = 0
  URL.createObjectURL = vi.fn(() => `blob:picture-${++made}`)
})

/**
 * Covers {@link renderDocument} (D2.6).
 *
 * Scenarios:
 * - a Word document is laid out by docx-preview, with its headings, table and picture, and bullets of the Symbol font become Unicode
 * - a hostile Word document renders without the HTML it embeds, and its remote picture never gets a network address
 * - an OpenDocument text becomes headings, styled spans, a centered paragraph, a list, a table and its picture
 * - OpenDocument spaces, tabs, line breaks, links, sections and frames are kept, unknown elements and missing pictures are left out,
 *   heading levels stay between 1 and 6, and only trusted styles and colors are applied
 * - an OpenDocument without a text body fails
 * - private characters of other symbol fonts become plain bullets
 */
describe('renderDocument', () => {
  it('lays out a Word document', async () => {
    await renderDocument(sample('document.docx'), root)
    expect(root.classList.contains('document')).toBe(true)
    expect(root.querySelector('.docx-pages section')).not.toBeNull()
    expect(root.textContent).toContain('Sentinel preview sample')
    expect(root.querySelector('table')?.textContent).toContain('Bogotá')
    expect(root.querySelector('img')?.getAttribute('src')).toMatch(/^blob:/)
    const css = Array.from(root.querySelectorAll('style'), (style) => style.textContent).join('\n')
    expect(css).toContain('•')
    expect(css).not.toMatch(/[-]/)
  })

  it('renders a hostile Word document without what it embeds', async () => {
    await renderDocument(sample('hostile.docx'), root)
    expect(root.textContent).toContain('Hostile document')
    expect(root.textContent).not.toContain('embedded html')
    expect(root.querySelector('script')).toBeNull()
    for (const image of Array.from(root.querySelectorAll('img'))) {
      expect(image.getAttribute('src') ?? '').not.toMatch(/^https?:/)
    }
  })

  it('reads an OpenDocument text', async () => {
    await renderDocument(sample('document.odt'), root)
    const page = root.querySelector('.odt-page') as HTMLElement
    expect(page.querySelector('h1')?.textContent).toBe('Sentinel preview sample')
    expect((page.querySelector('p span') as HTMLElement).style.fontWeight).toBe('bold')
    expect(Array.from(page.querySelectorAll('p')).find((p) => p.textContent === 'A centered paragraph.')?.style.textAlign).toBe('center')
    expect(page.querySelectorAll('li')).toHaveLength(3)
    expect(page.querySelector('table')?.textContent).toContain('Bogotá')
    expect(page.querySelector('img')?.getAttribute('src')).toBe('blob:picture-1')
  })

  it('keeps only what an OpenDocument text may safely say', async () => {
    const styles = [
      '<style:style style:name="Mixed"><style:text-properties fo:font-style="italic" style:text-underline-style="solid" fo:font-size="14pt" fo:color="red"/><style:paragraph-properties fo:text-align="end"/></style:style>',
      '<style:style style:name="Bad"><style:text-properties fo:font-size="expression(alert(1))" style:text-underline-style="none"/><style:paragraph-properties fo:text-align="url(x)"/></style:style>',
      '<style:style><style:text-properties/></style:style>',
    ].join('')
    const body = [
      '<office:text>',
      '<text:h>No level</text:h><text:h text:outline-level="9">Deep</text:h>',
      '<text:p text:style-name="Mixed">a<text:s text:c="3"/>b<text:s/>c<text:tab/>d<text:line-break/>e <text:a xlink:href="javascript:alert(1)">link</text:a></text:p>',
      '<text:p text:style-name="Bad">bad</text:p><text:p text:style-name="Unknown">plain</text:p>',
      '<text:section><text:p>in a section</text:p></text:section>',
      '<text:p><draw:frame><draw:image xlink:href="Pictures/missing.png"/></draw:frame><draw:frame><draw:image xlink:href="Pictures/note.txt"/></draw:frame></text:p>',
      '<table:table><table:table-header-rows><table:table-row><table:table-cell><text:p>head</text:p></table:table-cell></table:table-row></table:table-header-rows></table:table>',
      '<text:unknown>dropped</text:unknown><!-- a comment -->',
      '</office:text>',
    ].join('')
    await renderDocument(await odt(styles, body, { 'Pictures/note.txt': 'not a picture' }), root)
    const page = root.querySelector('.odt-page') as HTMLElement
    expect(page.querySelector('h1')?.textContent).toBe('No level')
    expect(page.querySelector('h6')?.textContent).toBe('Deep')
    const mixed = page.querySelectorAll('p')[0]
    expect(mixed.textContent).toBe('a   b c\td' + 'e link')
    expect(mixed.querySelector('br')).not.toBeNull()
    expect(mixed.style.cssText).toContain('italic')
    expect(mixed.style.textDecoration).toBe('underline')
    expect(mixed.style.fontSize).toBe('14pt')
    expect(mixed.style.textAlign).toBe('end')
    expect(mixed.style.color).toBe('')
    expect(mixed.querySelector('.odt-link')?.textContent).toBe('link')
    expect(mixed.querySelector('a')).toBeNull()
    const bad = page.querySelectorAll('p')[1]
    expect(bad.style.cssText).toBe('')
    expect(page.textContent).toContain('in a section')
    expect(page.querySelector('img')).toBeNull()
    expect(page.querySelector('table')?.textContent).toBe('head')
    expect(page.textContent).not.toContain('dropped')
  })

  it('refuses an OpenDocument without a text body', async () => {
    const zip = new JSZip()
    zip.file('mimetype', 'application/vnd.oasis.opendocument.text')
    await expect(renderDocument(await zip.generateAsync({ type: 'uint8array' }), root)).rejects.toThrow('not an OpenDocument text')
    await expect(renderDocument(await odt('', '<office:spreadsheet/>'), root)).rejects.toThrow('not an OpenDocument text')
  })

  it('turns unknown symbol characters into plain bullets', () => {
    const styles = document.createElement('div')
    styles.append(Object.assign(document.createElement('style'), { textContent: 'p:before { content: "\uf001\uf0a7"; }' }))
    fixBullets(styles)
    expect(styles.textContent).toBe('p:before { content: "•▪"; }')
  })
})
