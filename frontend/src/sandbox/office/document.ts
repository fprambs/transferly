import JSZip from 'jszip'

import { element, safeColor } from './common'

/** The namespaces of OpenDocument the reader looks for. */
const ns = {
  office: 'urn:oasis:names:tc:opendocument:xmlns:office:1.0',
  text: 'urn:oasis:names:tc:opendocument:xmlns:text:1.0',
  table: 'urn:oasis:names:tc:opendocument:xmlns:table:1.0',
  draw: 'urn:oasis:names:tc:opendocument:xmlns:drawing:1.0',
  style: 'urn:oasis:names:tc:opendocument:xmlns:style:1.0',
  fo: 'urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0',
  xlink: 'http://www.w3.org/1999/xlink',
} as const

/** The look an OpenDocument style gives, as CSS properties the reader allows. */
type Look = Partial<Record<'fontWeight' | 'fontStyle' | 'textDecoration' | 'color' | 'textAlign' | 'fontSize', string>>

/** Reads the styles of an OpenDocument part into looks, by name, keeping only the properties and values the viewer trusts. */
function readStyles(xml: Document, into: Map<string, Look>): void {
  for (const style of Array.from(xml.getElementsByTagNameNS(ns.style, 'style'))) {
    const look: Look = {}
    for (const props of Array.from(style.children)) {
      const weight = props.getAttributeNS(ns.fo, 'font-weight')
      const italic = props.getAttributeNS(ns.fo, 'font-style')
      const color = safeColor(props.getAttributeNS(ns.fo, 'color'))
      const align = props.getAttributeNS(ns.fo, 'text-align')
      const size = props.getAttributeNS(ns.fo, 'font-size')
      if (weight === 'bold') look.fontWeight = 'bold'
      if (italic === 'italic') look.fontStyle = 'italic'
      if ((props.getAttributeNS(ns.style, 'text-underline-style') ?? 'none') !== 'none') look.textDecoration = 'underline'
      if (color !== undefined) look.color = color
      if (align !== null && ['start', 'end', 'left', 'right', 'center', 'justify'].includes(align)) look.textAlign = align
      if (size !== null && /^\d+(\.\d+)?pt$/.test(size)) look.fontSize = size
    }
    into.set(style.getAttributeNS(ns.style, 'name') ?? '', look)
  }
}

/** Walks the content of an OpenDocument text, turning each element into plain HTML. */
class OdtReader {
  /** The looks of the styles, by name. */
  private readonly looks = new Map<string, Look>()
  /** The pictures of the document, by path. */
  private readonly pictures: Map<string, string>

  /** Reads with the styles of both parts and the pictures already turned into addresses. */
  constructor(parts: Document[], pictures: Map<string, string>) {
    parts.forEach((part) => readStyles(part, this.looks))
    this.pictures = pictures
  }

  /** Applies the look of the style an element names. */
  private style(from: Element, to: HTMLElement): void {
    const name = from.getAttributeNS(ns.text, 'style-name') ?? from.getAttributeNS(ns.table, 'style-name') ?? from.getAttributeNS(ns.draw, 'style-name')
    Object.assign(to.style, this.looks.get(name ?? '') ?? {})
  }

  /** Converts the children of an element into `into`. */
  children(from: Node, into: HTMLElement): void {
    for (const child of Array.from(from.childNodes)) {
      const converted = this.node(child)
      if (converted !== null) {
        into.append(converted)
      }
    }
  }

  /** Converts one node, or answers null for what the viewer leaves out. */
  node(node: Node): Node | null {
    if (node.nodeType === Node.TEXT_NODE) {
      return document.createTextNode(node.textContent as string)
    }
    if (node.nodeType !== Node.ELEMENT_NODE) {
      return null
    }
    const from = node as Element
    const tag = `${from.namespaceURI}|${from.localName}`
    let to: HTMLElement
    switch (tag) {
      case `${ns.text}|h`:
        to = element(`h${Math.min(Math.max(Number(from.getAttributeNS(ns.text, 'outline-level')) || 1, 1), 6)}` as 'h1')
        break
      case `${ns.text}|p`:
        to = element('p')
        break
      case `${ns.text}|span`:
      case `${ns.text}|a`:
        to = element('span', tag.endsWith('|a') ? 'odt-link' : undefined)
        break
      case `${ns.text}|list`:
        to = element('ul')
        break
      case `${ns.text}|list-item`:
        to = element('li')
        break
      case `${ns.text}|s`:
        return document.createTextNode(' '.repeat(Math.min(Number(from.getAttributeNS(ns.text, 'c')) || 1, 100)))
      case `${ns.text}|tab`:
        return document.createTextNode('\t')
      case `${ns.text}|line-break`:
        return element('br')
      case `${ns.table}|table`:
        to = element('table', 'odt-table')
        break
      case `${ns.table}|table-row`:
        to = element('tr')
        break
      case `${ns.table}|table-cell`:
        to = element('td')
        break
      case `${ns.table}|table-header-rows`:
      case `${ns.table}|table-rows`:
      case `${ns.draw}|frame`:
      case `${ns.text}|section`:
        to = element('span')
        break
      case `${ns.draw}|image`: {
        const address = this.pictures.get(from.getAttributeNS(ns.xlink, 'href') as string)
        if (address === undefined) {
          return null
        }
        const image = element('img', 'odt-image')
        image.src = address
        image.alt = ''
        return image
      }
      default:
        return null
    }
    this.style(from, to)
    this.children(from, to)
    return to
  }
}

/** The media types of the pictures an OpenDocument may hold, by extension. */
const pictureTypes: Record<string, string> = { png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', svg: 'image/svg+xml', webp: 'image/webp' }

/** Renders an OpenDocument text from its content and styles, with its pictures, as plain HTML on a page. */
async function renderOdt(zip: JSZip, root: HTMLElement): Promise<void> {
  const parse = async (name: string): Promise<Document | null> => {
    const file = zip.file(name)
    return file === null ? null : new DOMParser().parseFromString(await file.async('string'), 'application/xml')
  }
  const content = await parse('content.xml')
  const body = content?.getElementsByTagNameNS(ns.office, 'text')[0]
  if (body === undefined) {
    throw new Error('not an OpenDocument text')
  }
  const styles = await parse('styles.xml')
  const pictures = new Map<string, string>()
  for (const name of Object.keys(zip.files).filter((path) => path.startsWith('Pictures/'))) {
    const type = pictureTypes[(name.split('.').pop() as string).toLowerCase()]
    if (type !== undefined) {
      pictures.set(name, URL.createObjectURL(new Blob([(await (zip.file(name) as JSZip.JSZipObject).async('uint8array')) as Uint8Array<ArrayBuffer>], { type })))
    }
  }
  const page = element('article', 'odt-page')
  new OdtReader(styles === null ? [body.ownerDocument] : [styles, body.ownerDocument], pictures).children(body, page)
  root.classList.add('document')
  root.append(page)
}

/** The bullets of the Symbol and Wingdings fonts, which Word writes as private characters, and the Unicode characters they draw. */
const symbolBullets: Record<string, string> = { '\uf0b7': '•', '\uf0a7': '▪', '\uf06f': '◦', '\uf076': '❖', '\uf0d8': '➢', '\uf0fc': '✓', '\uf0a8': '◆' }

/** Replaces the private characters of Symbol and Wingdings bullets in the styles docx-preview wrote, since those fonts are rarely installed. */
export function fixBullets(styles: HTMLElement): void {
  for (const style of Array.from(styles.querySelectorAll('style'))) {
    style.textContent = (style.textContent as string).replace(/[\uf000-\uf0ff]/g, (char) => symbolBullets[char] ?? '•')
  }
}

/**
 * Renders a text document inside the isolated page (ADR-0018). A Word document (docx) is laid out page by page by docx-preview, with its
 * styles, tables, pictures, headers and footers (bullets drawn with Symbol or Wingdings become their Unicode characters); the HTML a document may embed (`altChunk`) is not rendered, and pictures come from the
 * file only, so links to the network do not load under the policy of the page. An OpenDocument text (odt) is read here from its XML into
 * plain HTML: headings, paragraphs, styled spans, lists, tables and pictures, keeping only the styles and colors the viewer trusts.
 */
export async function renderDocument(bytes: Uint8Array, root: HTMLElement): Promise<void> {
  const zip = await JSZip.loadAsync(bytes)
  const mimetype = await zip.file('mimetype')?.async('string')
  if (mimetype === 'application/vnd.oasis.opendocument.text') {
    await renderOdt(zip, root)
    return
  }
  const { renderAsync } = await import('docx-preview')
  const styles = element('div')
  const pages = element('div', 'docx-pages')
  root.classList.add('document')
  root.append(styles, pages)
  await renderAsync(bytes, pages, styles, { inWrapper: true, renderAltChunks: false, renderComments: false, renderChanges: false, useBase64URL: false, experimental: false })
  fixBullets(styles)
}
