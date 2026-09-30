import { getDocument, GlobalWorkerOptions, PasswordException, PDFDataRangeTransport, type PDFDocumentProxy, type PDFPageProxy } from 'pdfjs-dist'
import PdfWorker from 'pdfjs-dist/build/pdf.worker.min.mjs?worker&inline'

/** Reads a range of the file being shown, through the parent: the page itself has no network. */
export type RangeReader = (begin: number, end: number) => Promise<Uint8Array>

/** The zoom steps, as a scale of the page. */
const steps = [0.5, 0.75, 1, 1.25, 1.5, 2, 3]

/** The scale of the thumbnails. */
const thumbScale = 0.2

/** Bytes pdf.js asks for at once. */
const chunkSize = 64 * 1024

/** A PDF that asks for a password, which the viewer does not open. Its name tells it apart across the chunks of the page. */
export class ProtectedPdf extends Error {
  /** Names the error. */
  override name = 'ProtectedPdf'
}

/** Creates an element with a class and, optionally, its text; text is always set as text, never as HTML. */
function element<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag)
  node.className = className
  if (text !== undefined) {
    node.textContent = text
  }
  return node
}

/** Encodes text as base64, by chunks, since the code of the worker is too long to spread into one call. */
function base64(text: string): string {
  const bytes = new TextEncoder().encode(text)
  let binary = ''
  for (let at = 0; at < bytes.length; at += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000))
  }
  return btoa(binary)
}

/**
 * Starts the worker of pdf.js. The build inlines it into the page, since the page cannot load it from the network. The development server
 * of Vite serves it from its own address instead, transformed for its client, and a page with an opaque origin may start neither a worker
 * from another address nor a module worker from a `blob:` it made, so in development the page takes the untouched code of the worker as
 * text and starts it from a `data:` address, which belongs to no origin; the build drops this branch.
 */
export async function startWorker(development = import.meta.env.DEV): Promise<Worker> {
  if (!import.meta.env.DEV || !development) {
    return new PdfWorker()
  }
  const { default: code } = await import('pdfjs-dist/build/pdf.worker.min.mjs?raw')
  return new Worker(`data:text/javascript;base64,${base64(code)}`, { type: 'module' })
}

/** Starts the worker of pdf.js once. */
async function ensureWorker(): Promise<void> {
  if (GlobalWorkerOptions.workerPort === null) {
    GlobalWorkerOptions.workerPort = await startWorker()
  }
}

/** Draws one page onto a canvas at a scale. */
async function draw(page: PDFPageProxy, scale: number, canvas: HTMLCanvasElement): Promise<void> {
  const viewport = page.getViewport({ scale })
  canvas.width = Math.floor(viewport.width)
  canvas.height = Math.floor(viewport.height)
  await page.render({ canvas, viewport }).promise
}

/** Reads the text of a page, joined, for the search. */
async function pageText(page: PDFPageProxy): Promise<string> {
  const content = await page.getTextContent()
  return content.items.map((item) => ('str' in item ? item.str : '')).join(' ')
}

/**
 * Opens a PDF inside the isolated page (ADR-0018) with pdf.js, reading it by ranges through the parent, so a large file shows its first
 * page without being fetched whole. Pages are drawn onto canvases as they come into view; the bar moves between pages, zooms, fits the
 * width, searches the text and shows thumbnails. JavaScript of the document never runs: the display layer of pdf.js does not run it, and
 * forms and XFA are left off. A PDF that asks for a password fails with {@link ProtectedPdf}.
 */
export async function renderPdf(first: Uint8Array, size: number, read: RangeReader, root: HTMLElement, labels: Record<string, string>): Promise<void> {
  await ensureWorker()
  const transport = new PDFDataRangeTransport(size, first)
  transport.requestDataRange = (begin: number, end: number): void => {
    void read(begin, end).then((chunk) => transport.onDataRange(begin, chunk))
  }
  let doc: PDFDocumentProxy
  try {
    doc = await getDocument({ range: transport, rangeChunkSize: chunkSize, disableAutoFetch: true, useSystemFonts: true, useWasm: false, enableXfa: false, stopAtErrors: false }).promise
  } catch (error) {
    throw error instanceof PasswordException ? new ProtectedPdf() : error
  }
  const pages = await Promise.all(Array.from({ length: doc.numPages }, (_, index) => doc.getPage(index + 1)))
  let scale = 1.25
  const texts = new Map<number, string>()

  const bar = element('header', 'pdf-bar')
  const thumbsToggle = element('button', 'pdf-button', labels.thumbnails)
  const position = element('span', 'pdf-position')
  const zoomOut = element('button', 'pdf-button', '−')
  const zoomLabel = element('span', 'pdf-zoom')
  const zoomIn = element('button', 'pdf-button', '+')
  const fit = element('button', 'pdf-button', labels.fitWidth)
  const search = element('input', 'pdf-search')
  const found = element('span', 'pdf-found')
  zoomOut.setAttribute('aria-label', labels.zoomOut)
  zoomIn.setAttribute('aria-label', labels.zoomIn)
  search.type = 'search'
  search.placeholder = labels.search
  search.setAttribute('aria-label', labels.search)
  bar.append(thumbsToggle, position, zoomOut, zoomLabel, zoomIn, fit, search, found)

  const body = element('div', 'pdf-body')
  const thumbs = element('nav', 'pdf-thumbs')
  thumbs.hidden = true
  thumbs.setAttribute('aria-label', labels.thumbnails)
  const sheet = element('div', 'pdf-pages')
  body.append(thumbs, sheet)
  root.replaceChildren(bar, body)
  root.classList.add('pdf')

  const frames = pages.map((_page, index) => {
    const frame = element('section', 'pdf-page')
    frame.setAttribute('aria-label', labels.page.replace('{{number}}', String(index + 1)))
    frame.dataset.page = String(index + 1)
    sheet.append(frame)
    return frame
  })
  const drawn = new Set<number>()
  const layout = (): void => {
    drawn.clear()
    frames.forEach((frame, index) => {
      const viewport = pages[index].getViewport({ scale })
      frame.style.width = `${Math.floor(viewport.width)}px`
      frame.style.height = `${Math.floor(viewport.height)}px`
      frame.replaceChildren()
    })
    zoomLabel.textContent = `${Math.round(scale * 100)}%`
  }
  const drawVisible = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        const index = Number((entry.target as HTMLElement).dataset.page) - 1
        if (entry.isIntersecting && !drawn.has(index)) {
          drawn.add(index)
          const canvas = element('canvas', 'pdf-canvas')
          entry.target.replaceChildren(canvas)
          void draw(pages[index], scale, canvas)
        }
      }
      const visible = entries.filter((entry) => entry.isIntersecting).map((entry) => Number((entry.target as HTMLElement).dataset.page))
      if (visible.length > 0) {
        position.textContent = labels.position.replace('{{page}}', String(Math.min(...visible))).replace('{{total}}', String(pages.length))
      }
    },
    { root: sheet, rootMargin: '400px 0px' },
  )
  layout()
  frames.forEach((frame) => drawVisible.observe(frame))
  position.textContent = labels.position.replace('{{page}}', '1').replace('{{total}}', String(pages.length))

  const zoomTo = (next: number): void => {
    scale = next
    layout()
    frames.forEach((frame) => {
      drawVisible.unobserve(frame)
      drawVisible.observe(frame)
    })
  }
  zoomIn.addEventListener('click', () => zoomTo(steps.find((step) => step > scale) ?? scale))
  zoomOut.addEventListener('click', () => zoomTo([...steps].reverse().find((step) => step < scale) ?? scale))
  fit.addEventListener('click', () => zoomTo((sheet.clientWidth - 48) / pages[0].getViewport({ scale: 1 }).width))

  let thumbsDrawn = false
  thumbsToggle.addEventListener('click', () => {
    thumbs.hidden = !thumbs.hidden
    if (thumbs.hidden || thumbsDrawn) {
      return
    }
    thumbsDrawn = true
    pages.forEach((page, index) => {
      const button = element('button', 'pdf-thumb')
      const canvas = element('canvas', 'pdf-canvas')
      button.append(canvas, element('span', 'pdf-thumb-number', String(index + 1)))
      button.addEventListener('click', () => frames[index].scrollIntoView())
      thumbs.append(button)
      void draw(page, thumbScale, canvas)
    })
  })

  let matches: number[] = []
  let current = -1
  search.addEventListener('keydown', (event) => {
    if (event.key !== 'Enter') {
      return
    }
    void (async () => {
      const query = search.value.trim().toLowerCase()
      if (query === '') {
        found.textContent = ''
        return
      }
      matches = []
      for (const [index, page] of pages.entries()) {
        if (!texts.has(index)) {
          texts.set(index, (await pageText(page)).toLowerCase())
        }
        if ((texts.get(index) as string).includes(query)) {
          matches.push(index)
        }
      }
      current = matches.length === 0 ? -1 : (current + 1) % matches.length
      found.textContent = matches.length === 0 ? labels.noMatches : labels.matches.replace('{{page}}', String(matches[current] + 1)).replace('{{total}}', String(matches.length))
      if (current >= 0) {
        frames[matches[current]].scrollIntoView()
      }
    })()
  })
}
