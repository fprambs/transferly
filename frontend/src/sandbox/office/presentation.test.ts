import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import JSZip from 'jszip'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { renderPresentation } from './presentation'

/** The words of the interface, as the parent hands them. */
const labels = { slides: 'Slides: {{total}}', slide: 'Slide {{number}}' }

/** Reads an Office sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/office', name)))
}

/** The namespaces a deck written by hand declares. */
const declarations =
  'xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ' +
  'xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"'

/** A relationships part. */
function rels(entries: Array<[string, string, string]>): string {
  const body = entries.map(([id, type, target]) => `<Relationship Id="${id}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/${type}" Target="${target}"/>`)
  return `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${body.join('')}</Relationships>`
}

/** A transform at a place and of a size, in EMU. */
function xfrm(x: number, y: number, cx: number, cy: number, prefix = 'a'): string {
  return `<${prefix}:xfrm><a:off x="${x}" y="${y}"/><a:ext cx="${cx}" cy="${cy}"/></${prefix}:xfrm>`
}

/** Writes a deck by hand: the parts given, by path, around a presentation of the given slides and size. */
async function deck(slides: string[], parts: Record<string, string | Uint8Array>, size = '<p:sldSz cx="1000000" cy="500000"/>'): Promise<Uint8Array> {
  const zip = new JSZip()
  const ids = slides.map((_, index) => `<p:sldId id="${256 + index}" r:id="rId${index + 1}"/>`).join('')
  zip.file('ppt/presentation.xml', `<p:presentation ${declarations}><p:sldIdLst>${ids}<p:sldId id="999" r:id="rIdMissing"/><p:sldId id="1000"/></p:sldIdLst>${size}</p:presentation>`)
  zip.file('ppt/_rels/presentation.xml.rels', rels(slides.map((path, index) => [`rId${index + 1}`, 'slide', path])))
  for (const [path, content] of Object.entries(parts)) {
    zip.file(path, content)
  }
  return zip.generateAsync({ type: 'uint8array' })
}

/** A slide with the given shapes and, optionally, a background. */
function slide(shapes: string, background = ''): string {
  return `<p:sld ${declarations}><p:cSld>${background}<p:spTree>${shapes}</p:spTree></p:cSld></p:sld>`
}

let root: HTMLElement

beforeEach(() => {
  root = document.createElement('div')
  let made = 0
  URL.createObjectURL = vi.fn(() => `blob:picture-${++made}`)
})

/**
 * Covers {@link renderPresentation} (D2.6).
 *
 * Scenarios:
 * - a PowerPoint deck shows how many slides it has and each slide at its proportions: placeholders placed from their layout, a title,
 *   bullets, a picture of the package, a filled shape with bold text, a text box in italics and a table with its header row
 * - slides without a layout, placeholders placed from the master, groups, backgrounds, alignment, underline, theme colors, empty
 *   paragraphs, fields and centered text are drawn; shapes with no place, pictures missing or not images, charts and connectors are left
 *   out, as are slides the presentation lists but the package lacks
 * - a deck with no slide size takes the default one, and a package without a presentation fails
 */
describe('renderPresentation', () => {
  it('lays out a PowerPoint deck', async () => {
    await renderPresentation(sample('slides.pptx'), root, labels)
    expect(root.querySelector('.slides-summary')?.textContent).toBe('Slides: 4')
    const slides = Array.from(root.querySelectorAll('.slide'))
    expect(slides.map((node) => node.getAttribute('aria-label'))).toEqual(['Slide 1', 'Slide 2', 'Slide 3', 'Slide 4'])
    expect(slides[0].textContent).toContain('Sentinel preview sample')
    expect((slides[0].querySelector('.slide-canvas') as HTMLElement).style.aspectRatio).toBe('12191695 / 6858000')
    expect(slides[1].querySelectorAll('.slide-bullet').length).toBeGreaterThanOrEqual(4)
    expect(slides[2].querySelector('img')?.getAttribute('src')).toBe('blob:picture-1')
    const filled = Array.from(slides[2].querySelectorAll<HTMLElement>('.slide-shape')).find((shape) => shape.textContent === 'A filled shape') as HTMLElement
    expect(filled.style.background).toContain('rgb(14, 159, 110)')
    expect(filled.classList.contains('slide-middle')).toBe(true)
    expect((filled.querySelector('span') as HTMLElement).style.fontWeight).toBe('bold')
    expect((filled.querySelector('span') as HTMLElement).style.color).toBe('rgb(255, 255, 255)')
    expect((Array.from(slides[2].querySelectorAll('span')).find((span) => span.textContent?.includes('italics')) as HTMLElement).style.fontStyle).toBe('italic')
    expect(slides[3].querySelectorAll('.slide-table tr')).toHaveLength(5)
  })

  it('draws what a slide may hold and leaves out the rest', async () => {
    const title = '<p:sp><p:nvSpPr><p:cNvPr id="2" name="t"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:p><a:r><a:t>From the master</a:t></a:r></a:p></p:txBody></p:sp>'
    const text = [
      '<p:sp><p:nvSpPr><p:cNvPr id="3" name="x"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>',
      `<p:spPr>${xfrm(0, 0, 500000, 250000)}<a:solidFill><a:srgbClr val="javascript"/></a:solidFill></p:spPr>`,
      '<p:txBody><a:bodyPr anchor="t"/>',
      '<a:p><a:pPr algn="ctr"/><a:r><a:rPr u="sng"><a:solidFill><a:schemeClr val="dk1"/></a:solidFill></a:rPr><a:t>centered</a:t></a:r></a:p>',
      '<a:p><a:pPr algn="r"/><a:fld type="slidenum"><a:t>7</a:t></a:fld></a:p>',
      '<a:p><a:pPr algn="dist"/></a:p>',
      '<a:p><a:r><a:rPr><a:solidFill><a:schemeClr val="accent1"/></a:solidFill></a:rPr><a:t>themed</a:t></a:r><a:r><a:rPr/></a:r></a:p>',
      '</p:txBody></p:sp>',
    ].join('')
    const body = '<p:sp><p:nvSpPr><p:cNvPr id="6" name="b"/><p:cNvSpPr/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:p><a:pPr lvl="1"/><a:r><a:t>second level</a:t></a:r></a:p></p:txBody></p:sp>'
    const nowhere = '<p:sp><p:nvSpPr><p:cNvPr id="4" name="n"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr/></p:sp>'
    const orphan = '<p:sp><p:nvSpPr><p:cNvPr id="5" name="o"/><p:cNvSpPr/><p:nvPr><p:ph type="dt"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>'
    const pictures = ['rIdMissing', 'rIdText', 'rIdGone']
      .map((id) => `<p:pic><p:nvPicPr><p:cNvPr id="7" name="p"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="${id}"/></p:blipFill><p:spPr>${xfrm(0, 0, 10, 10)}</p:spPr></p:pic>`)
      .join('')
    const bare = `<p:pic><p:nvPicPr><p:cNvPr id="13" name="p"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:spPr>${xfrm(0, 0, 10, 10)}</p:spPr></p:pic>`
    const plain = `<p:sp><p:nvSpPr><p:cNvPr id="14" name="s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr>${xfrm(0, 0, 10, 10)}</p:spPr></p:sp>`
    const picture = `<p:pic><p:nvPicPr><p:cNvPr id="8" name="p"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rIdPicture"/></p:blipFill><p:spPr>${xfrm(0, 0, 10, 10)}</p:spPr></p:pic>`
    const chart = `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="9" name="c"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>${xfrm(0, 0, 10, 10, 'p')}<a:graphic><a:graphicData uri="chart"/></a:graphic></p:graphicFrame>`
    const table = `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="10" name="t"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>${xfrm(0, 0, 10, 10, 'p')}<a:graphic><a:graphicData uri="table"><a:tbl><a:tr><a:tc><a:txBody><a:p><a:r><a:t>cell</a:t></a:r></a:p></a:txBody></a:tc><a:tc/></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
    const connector = `<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="11" name="l"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr>${xfrm(0, 0, 10, 10)}</p:spPr></p:cxnSp>`
    const group = `<p:grpSp><p:nvGrpSpPr><p:cNvPr id="12" name="g"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>${text}</p:grpSp>`
    const master = `<p:sldMaster ${declarations}><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="2" name="t"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr>${xfrm(100, 100, 800000, 100000)}</p:spPr></p:sp><p:sp><p:nvSpPr><p:cNvPr id="3" name="b"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr>${xfrm(100, 200000, 800000, 200000)}</p:spPr></p:sp></p:spTree></p:cSld></p:sldMaster>`
    const layout = `<p:sldLayout ${declarations}><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="2" name="t"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp></p:spTree></p:cSld></p:sldLayout>`
    const bytes = await deck(['slides/one.xml', 'slides/two.xml', 'slides/gone.xml', 'slides/three.xml'], {
      'ppt/slides/one.xml': slide(title + body + text + nowhere + orphan + pictures + bare + plain + picture + picture + chart + table + connector + group, '<p:bg><p:bgPr><a:solidFill><a:srgbClr val="112233"/></a:solidFill></p:bgPr></p:bg>'),
      'ppt/slides/_rels/one.xml.rels': rels([['rIdLayout', 'slideLayout', '../slideLayouts/layout.xml'], ['rIdText', 'image', '../media/note.txt'], ['rIdGone', 'image', '../media/gone.png'], ['rIdPicture', 'image', './../media/photo.png']]),
      'ppt/slideLayouts/layout.xml': layout,
      'ppt/slideLayouts/_rels/layout.xml.rels': rels([['rIdMaster', 'slideMaster', '../slideMasters/master.xml']]),
      'ppt/slideMasters/master.xml': master,
      'ppt/media/note.txt': 'not a picture',
      'ppt/media/photo.png': new Uint8Array([0x89, 0x50, 0x4e, 0x47]),
      'ppt/slides/two.xml': slide(title),
      'ppt/slides/three.xml': `<p:sld ${declarations}><p:cSld/></p:sld>`,
    })
    await renderPresentation(bytes, root, labels)
    expect(root.querySelector('.slides-summary')?.textContent).toBe('Slides: 4')
    const [one, two, three] = Array.from(root.querySelectorAll<HTMLElement>('.slide'))
    expect(root.querySelectorAll('.slide')).toHaveLength(3)
    expect(three.textContent).toBe('')
    expect((one.querySelector('.slide-canvas') as HTMLElement).style.background).toContain('rgb(17, 34, 51)')
    const shapes = Array.from(one.querySelectorAll<HTMLElement>('.slide-shape'))
    const placed = shapes.find((shape) => shape.textContent === 'From the master') as HTMLElement
    expect([placed.style.left, placed.style.width]).toEqual(['0.01%', '80%'])
    const second = shapes.find((shape) => shape.textContent === 'second level') as HTMLElement
    expect(second.querySelector('.slide-bullet')?.getAttribute('style')).toContain('margin-inline-start: 4%')
    const texts = shapes.filter((shape) => shape.textContent?.startsWith('centered'))
    expect(texts).toHaveLength(2)
    const [centered, number, empty, themed] = Array.from(texts[0].querySelectorAll<HTMLElement>('p'))
    expect([centered.style.textAlign, number.style.textAlign, empty.style.textAlign]).toEqual(['center', 'right', ''])
    expect((centered.querySelector('span') as HTMLElement).style.textDecoration).toBe('underline')
    expect((centered.querySelector('span') as HTMLElement).style.color).toBe('rgb(0, 0, 0)')
    expect(number.textContent).toBe('7')
    expect(empty.querySelector('br')).not.toBeNull()
    expect((themed.querySelector('span') as HTMLElement).style.color).toBe('')
    expect(texts[0].style.background).toBe('')
    expect(one.querySelectorAll('img')).toHaveLength(2)
    expect(URL.createObjectURL).toHaveBeenCalledTimes(1)
    expect(one.querySelector('.slide-table')?.textContent).toBe('cell')
    expect(two.textContent).toBe('')
  })

  it('takes the default size and refuses a package without a presentation', async () => {
    await renderPresentation(await deck([], {}, ''), root, labels)
    expect(root.querySelector('.slides-summary')?.textContent).toBe('Slides: 0')
    await expect(renderPresentation(await new JSZip().generateAsync({ type: 'uint8array' }), root, labels)).rejects.toThrow('not a PowerPoint deck')
  })
})
