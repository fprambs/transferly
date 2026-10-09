import { checkOfficeBudget } from './budget'
import JSZip from 'jszip'

import { element, fill, safeColor } from './common'

/** The namespaces of PresentationML the reader looks for. */
const ns = {
  p: 'http://schemas.openxmlformats.org/presentationml/2006/main',
  a: 'http://schemas.openxmlformats.org/drawingml/2006/main',
  r: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships',
  rel: 'http://schemas.openxmlformats.org/package/2006/relationships',
} as const

/** The most slides the viewer draws. */
export const maxSlides = 500

/** Where a shape sits on its slide, in EMU. */
interface Box {
  /** Left edge. */
  x: number
  /** Top edge. */
  y: number
  /** Width. */
  width: number
  /** Height. */
  height: number
}

/** The media types of the pictures a deck may hold, by extension. */
const pictureTypes: Record<string, string> = { png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', bmp: 'image/bmp', svg: 'image/svg+xml', webp: 'image/webp' }

/** Theme colors a run or a shape may name, reduced to the two a deck almost always maps to light and dark. */
const schemeColors: Record<string, string> = { lt1: '#ffffff', bg1: '#ffffff', dk1: '#000000', tx1: '#000000' }

/** The size of the text of each kind of placeholder when the deck does not say, in points, as the default template has it. */
const placeholderSizes: Record<string, number> = { title: 44, ctrTitle: 44, subTitle: 24, body: 24 }

/** Resolves a target of a relationship against the part it belongs to, as a path inside the package. */
function resolve(part: string, target: string): string {
  const parts = part.split('/').slice(0, -1)
  for (const step of target.split('/')) {
    if (step === '..') parts.pop()
    else if (step !== '.') parts.push(step)
  }
  return parts.join('/')
}

/** The children of an element in a namespace, by local name. */
function childrenOf(parent: Element | null | undefined, namespace: string, name: string): Element[] {
  return Array.from(parent?.children ?? []).filter((child) => child.namespaceURI === namespace && child.localName === name)
}

/** The first child of an element in a namespace, by local name. */
function child(parent: Element | null | undefined, namespace: string, name: string): Element | undefined {
  return childrenOf(parent, namespace, name)[0]
}

/** The first descendant of an element in a namespace, by local name. */
function descendant(parent: Element | Document | null | undefined, namespace: string, name: string): Element | undefined {
  return parent?.getElementsByTagNameNS(namespace, name)[0]
}

/** Reads the color a fill or a run names: an RGB value or one of the theme colors the reader knows. */
function colorOf(parent: Element | undefined): string | undefined {
  const fillColor = child(parent, ns.a, 'solidFill')
  const rgb = child(fillColor, ns.a, 'srgbClr')?.getAttribute('val')
  return safeColor(rgb) ?? schemeColors[child(fillColor, ns.a, 'schemeClr')?.getAttribute('val') ?? '']
}

/** Reads the position of a transform, when it has one. */
function boxOf(transform: Element | undefined): Box | undefined {
  const offset = child(transform, ns.a, 'off')
  const extent = child(transform, ns.a, 'ext')
  if (offset === undefined || extent === undefined) {
    return undefined
  }
  const number = (node: Element, name: string): number => Number(node.getAttribute(name)) || 0
  return { x: number(offset, 'x'), y: number(offset, 'y'), width: number(extent, 'cx'), height: number(extent, 'cy') }
}

/** The placeholder a shape stands for: its type and index, when it is one. */
function placeholderOf(shape: Element): { type: string; index: string } | undefined {
  const holder = descendant(shape, ns.p, 'ph')
  return holder === undefined ? undefined : { type: holder.getAttribute('type') ?? 'body', index: holder.getAttribute('idx') ?? '' }
}

/** A deck opened: its package, the size of its slides, and the parts already read. */
class Deck {
  /** The package. */
  readonly zip: JSZip
  /** The width of a slide, in EMU. */
  width = 9_144_000
  /** The height of a slide, in EMU. */
  height = 6_858_000
  /** The parts read, by path. */
  private readonly parts = new Map<string, Promise<Document | null>>()
  /** The pictures turned into addresses, by path. */
  private readonly pictures = new Map<string, Promise<string | undefined>>()

  /** Opens a package. */
  constructor(zip: JSZip) {
    this.zip = zip
  }

  /** Reads an XML part, once, or answers null when the package lacks it. */
  part(path: string): Promise<Document | null> {
    let read = this.parts.get(path)
    if (read === undefined) {
      const file = this.zip.file(path)
      read = file === null ? Promise.resolve(null) : file.async('string').then((text) => new DOMParser().parseFromString(text, 'application/xml'))
      this.parts.set(path, read)
    }
    return read
  }

  /** The relationships of a part, as targets by identifier, and the first target of a type. */
  async relationships(path: string): Promise<{ byId: Map<string, string>; ofType: (type: string) => string | undefined }> {
    const folder = path.split('/').slice(0, -1).join('/')
    const rels = await this.part(`${folder}/_rels/${path.split('/').pop() as string}.rels`)
    const list = Array.from(rels?.getElementsByTagNameNS(ns.rel, 'Relationship') ?? [])
    const byId = new Map(list.map((rel) => [rel.getAttribute('Id') as string, resolve(path, rel.getAttribute('Target') as string)]))
    return { byId, ofType: (type) => list.find((rel) => String(rel.getAttribute('Type')).endsWith(`/${type}`))?.getAttribute('Target') as string | undefined }
  }

  /** Turns a picture of the package into a `blob:` address, once; a picture that is not in the package, or not an image, gives none. */
  picture(path: string): Promise<string | undefined> {
    let made = this.pictures.get(path)
    if (made === undefined) {
      const type = pictureTypes[(path.split('.').pop() as string).toLowerCase()]
      const file = this.zip.file(path)
      made = type === undefined || file === null ? Promise.resolve(undefined) : file.async('uint8array').then((data) => URL.createObjectURL(new Blob([data as Uint8Array<ArrayBuffer>], { type })))
      this.pictures.set(path, made)
    }
    return made
  }
}

/** Places an element on its slide, in hundredths of the size of the slide, so the slide scales with the page. */
function place(node: HTMLElement, box: Box, deck: Deck): void {
  node.style.left = `${(box.x / deck.width) * 100}%`
  node.style.top = `${(box.y / deck.height) * 100}%`
  node.style.width = `${(box.width / deck.width) * 100}%`
  node.style.height = `${(box.height / deck.height) * 100}%`
}

/** A size of text in points, as a length that scales with the width of the slide. */
function textSize(points: number, deck: Deck): string {
  return `${((points * 12_700) / deck.width) * 100}cqw`
}

/** Draws the paragraphs of a text body: alignment, bullets of body placeholders, and runs with their size, weight, slant, underline and color. */
function drawText(body: Element, into: HTMLElement, deck: Deck, defaults: { size: number; bullets: boolean; color?: string }): void {
  for (const paragraph of childrenOf(body, ns.a, 'p')) {
    const line = element('p', 'slide-paragraph')
    const props = child(paragraph, ns.a, 'pPr')
    const level = Number(props?.getAttribute('lvl')) || 0
    const align = { ctr: 'center', r: 'right', just: 'justify' }[props?.getAttribute('algn') ?? '']
    if (align !== undefined) line.style.textAlign = align
    const runs = [...childrenOf(paragraph, ns.a, 'r'), ...childrenOf(paragraph, ns.a, 'fld')]
    if (defaults.bullets && runs.length > 0) {
      line.classList.add('slide-bullet')
      line.style.marginInlineStart = `${level * 4}%`
    }
    const baseSize = defaults.size - (defaults.bullets ? level * 4 : 0)
    for (const run of runs) {
      const runProps = child(run, ns.a, 'rPr')
      const span = element('span', undefined, child(run, ns.a, 't')?.textContent ?? '')
      const size = Number(runProps?.getAttribute('sz')) / 100 || baseSize
      span.style.fontSize = textSize(size, deck)
      if (runProps?.getAttribute('b') === '1') span.style.fontWeight = 'bold'
      if (runProps?.getAttribute('i') === '1') span.style.fontStyle = 'italic'
      if ((runProps?.getAttribute('u') ?? 'none') !== 'none') span.style.textDecoration = 'underline'
      const color = colorOf(runProps) ?? defaults.color
      if (color !== undefined) span.style.color = color
      line.append(span)
    }
    if (runs.length === 0) {
      line.style.fontSize = textSize(baseSize, deck)
      line.append(element('br'))
    }
    into.append(line)
  }
}

/** Draws a table of a graphic frame: its rows and cells, with the text of each. */
function drawTable(table: Element, into: HTMLElement, deck: Deck): void {
  const grid = element('table', 'slide-table')
  for (const row of childrenOf(table, ns.a, 'tr')) {
    const line = element('tr')
    for (const cell of childrenOf(row, ns.a, 'tc')) {
      const box = element('td')
      const body = child(cell, ns.a, 'txBody')
      if (body !== undefined) {
        drawText(body, box, deck, { size: 18, bullets: false })
      }
      line.append(box)
    }
    grid.append(line)
  }
  into.append(grid)
}

/** Draws one slide: its background, then its shapes, pictures, tables and groups in the order they are stacked; a slide the package
 * lacks gives nothing. */
async function drawSlide(deck: Deck, path: string, number: number, labels: Record<string, string>): Promise<HTMLElement | null> {
  const slide = await deck.part(path)
  if (slide === null) {
    return null
  }
  const rels = await deck.relationships(path)
  const layoutPath = rels.ofType('slideLayout')
  const layout = layoutPath === undefined ? undefined : resolve(path, layoutPath)
  const layoutDoc = layout === undefined ? null : await deck.part(layout)
  const masterTarget = layout === undefined ? undefined : (await deck.relationships(layout)).ofType('slideMaster')
  const masterDoc = layout === undefined || masterTarget === undefined ? null : await deck.part(resolve(layout, masterTarget))
  const inherited = (holder: { type: string; index: string }): Box | undefined => {
    for (const doc of [layoutDoc, masterDoc]) {
      for (const shape of Array.from(doc?.getElementsByTagNameNS(ns.p, 'sp') ?? [])) {
        const own = placeholderOf(shape)
        const matches = own !== undefined && ((holder.index !== '' && own.index === holder.index) || own.type === holder.type)
        const box = matches ? boxOf(descendant(shape, ns.a, 'xfrm')) : undefined
        if (box !== undefined) return box
      }
    }
    return undefined
  }

  const frame = element('section', 'slide')
  frame.setAttribute('aria-label', fill(labels.slide, { number }))
  const canvas = element('div', 'slide-canvas')
  canvas.style.aspectRatio = `${deck.width} / ${deck.height}`
  canvas.style.background = colorOf(descendant(descendant(slide, ns.p, 'bg'), ns.p, 'bgPr')) ?? '#ffffff'
  frame.append(canvas)

  const drawShapes = async (tree: Element | undefined): Promise<void> => {
    for (const shape of Array.from(tree?.children ?? [])) {
      if (shape.localName === 'grpSp') {
        await drawShapes(shape)
        continue
      }
      const holder = placeholderOf(shape)
      const box = boxOf(descendant(shape, ns.a, 'xfrm') ?? child(shape, ns.p, 'xfrm')) ?? (holder === undefined ? undefined : inherited(holder))
      if (box === undefined) {
        continue
      }
      const node = element('div', 'slide-shape')
      place(node, box, deck)
      if (shape.localName === 'pic') {
        const embed = descendant(shape, ns.a, 'blip')?.getAttributeNS(ns.r, 'embed') ?? ''
        const target = rels.byId.get(embed)
        const address = target === undefined ? undefined : await deck.picture(target)
        if (address === undefined) continue
        const picture = element('img', 'slide-picture')
        picture.src = address
        picture.alt = descendant(shape, ns.p, 'cNvPr')?.getAttribute('descr') ?? ''
        node.append(picture)
      } else if (shape.localName === 'graphicFrame') {
        const table = descendant(shape, ns.a, 'tbl')
        if (table === undefined) continue
        drawTable(table, node, deck)
      } else if (shape.localName === 'sp') {
        const fillColor = colorOf(child(shape, ns.p, 'spPr'))
        if (fillColor !== undefined) node.style.background = fillColor
        const body = child(shape, ns.p, 'txBody')
        if (child(body, ns.a, 'bodyPr')?.getAttribute('anchor') === 'ctr') node.classList.add('slide-middle')
        const fontColor = schemeColors[descendant(descendant(shape, ns.a, 'fontRef'), ns.a, 'schemeClr')?.getAttribute('val') ?? '']
        if (body !== undefined) {
          drawText(body, node, deck, { size: placeholderSizes[holder?.type ?? ''] ?? 18, bullets: holder !== undefined && holder.type === 'body', color: fontColor })
        }
      } else {
        continue
      }
      canvas.append(node)
    }
  }
  await drawShapes(descendant(slide, ns.p, 'spTree'))
  return frame
}

/**
 * Renders a PowerPoint deck (pptx) inside the isolated page (ADR-0018), read here from its XML: each slide laid out at its own proportions
 * with its background, text boxes (sizes, weights, colors, alignment and bullets), filled shapes, pictures of the package, simple tables
 * and groups; placeholders take their place from the layout or the master when the slide does not say. Charts, animations, notes and
 * effects are left out, and at most {@link maxSlides} slides are drawn. Sizes are fractions of the slide, so it scales with the page.
 */
export async function renderPresentation(bytes: Uint8Array, root: HTMLElement, labels: Record<string, string>): Promise<void> {
  await checkOfficeBudget(bytes)
  const deck = new Deck(await JSZip.loadAsync(bytes))
  const presentation = await deck.part('ppt/presentation.xml')
  if (presentation === null) {
    throw new Error('not a PowerPoint deck')
  }
  const size = descendant(presentation, ns.p, 'sldSz')
  deck.width = Number(size?.getAttribute('cx')) || deck.width
  deck.height = Number(size?.getAttribute('cy')) || deck.height
  const rels = await deck.relationships('ppt/presentation.xml')
  const slides = Array.from(presentation.getElementsByTagNameNS(ns.p, 'sldId'))
    .map((id) => rels.byId.get(id.getAttributeNS(ns.r, 'id') ?? ''))
    .filter((path): path is string => path !== undefined)
  root.classList.add('slides')
  root.append(element('p', 'slides-summary', fill(labels.slides, { total: slides.length })))
  for (const [index, path] of slides.slice(0, maxSlides).entries()) {
    const drawn = await drawSlide(deck, path, index + 1, labels)
    if (drawn !== null) {
      root.append(drawn)
    }
  }
}
