import { beforeEach, describe, expect, it, vi } from 'vitest'

/** The fakes of pdf.js the renderer is driven with. */
const fakes = vi.hoisted(() => {
  class PDFDataRangeTransport {
    onDataRange = vi.fn()
    length: number
    initialData: Uint8Array
    constructor(length: number, initialData: Uint8Array) {
      this.length = length
      this.initialData = initialData
    }
    requestDataRange(begin: number, end: number): void {
      void [begin, end]
    }
  }
  return { getDocument: vi.fn(), GlobalWorkerOptions: { workerPort: null as unknown }, PasswordResponses: { NEED_PASSWORD: 1, INCORRECT_PASSWORD: 2 }, PDFDataRangeTransport }
})

vi.mock('pdfjs-dist', () => fakes)
vi.mock('pdfjs-dist/build/pdf.worker.min.mjs?worker&inline', () => ({ default: class {} }))
vi.mock('pdfjs-dist/build/pdf.worker.min.mjs?raw', () => ({ default: `self.onmessage = null // ${'ñ'.repeat(40_000)}` }))

import { renderPdf, startWorker } from './pdf'

/** The observers created by the renderer, to drive what comes into view. */
const observers: Array<{ callback: IntersectionObserverCallback; observed: Set<Element> }> = []

/** A fake page of `words`, 600 by 800 at scale 1. */
function page(words: string) {
  return {
    getViewport: ({ scale }: { scale: number }) => ({ width: 600 * scale, height: 800 * scale }),
    render: vi.fn(() => ({ promise: Promise.resolve() })),
    getTextContent: vi.fn(async () => ({ items: [{ str: words }, { type: 'beginMarkedContent' }] })),
  }
}

/** Labels as the parent sends them. */
const labels = {
  thumbnails: 'Pages',
  fitWidth: 'Fit width',
  zoomIn: 'Zoom in',
  zoomOut: 'Zoom out',
  search: 'Search',
  page: 'Page {{number}}',
  position: 'Page {{page}} of {{total}}',
  matches: 'Found on page {{page}} · {{total}} pages',
  noMatches: 'Not found',
  passwordTitle: 'Protected',
  passwordBody: 'Type it',
  password: 'Password',
  open: 'Open',
  wrongPassword: 'Wrong',
}

/** Opens a document of pages with the given words, and returns what the tests use. */
async function open(words: string[]) {
  const pages = words.map(page)
  fakes.getDocument.mockReturnValueOnce({ promise: Promise.resolve({ numPages: pages.length, getPage: async (number: number) => pages[number - 1] }) })
  const root = document.createElement('main')
  document.body.append(root)
  const read = vi.fn(async (begin: number, end: number) => new Uint8Array(end - begin))
  await renderPdf(new Uint8Array(10), 1000, read, root, labels)
  return { root, pages, read, observer: observers.at(-1) as (typeof observers)[number] }
}

/** Tells the observer that pages came into view, or left it. */
function see(observer: (typeof observers)[number], root: HTMLElement, visible: number[], hidden: number[] = []): void {
  const entry = (number: number, isIntersecting: boolean) => ({ target: root.querySelector(`[data-page="${number}"]`) as Element, isIntersecting })
  observer.callback([...visible.map((number) => entry(number, true)), ...hidden.map((number) => entry(number, false))] as IntersectionObserverEntry[], {} as IntersectionObserver)
}

/** Waits for pending promises. */
async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

/** A worker that only remembers where it was started from. */
class FakeWorker {
  /** The address it was started from. */
  url: string
  /** Its options. */
  options: WorkerOptions | undefined
  /** Remembers the address and the options. */
  constructor(url: string, options?: WorkerOptions) {
    this.url = url
    this.options = options
  }
}

/**
 * Covers {@link startWorker}.
 *
 * Scenarios:
 * - the build starts the worker inlined into the page
 * - development starts the untouched code of the worker, longer than one chunk and not only ASCII, as a module worker from a data address
 */
describe('startWorker', () => {
  it('starts the inlined worker in the build and a data worker in development', async () => {
    vi.stubGlobal('Worker', FakeWorker)
    expect(await startWorker(false)).not.toBeInstanceOf(FakeWorker)
    const worker = (await startWorker(true)) as unknown as FakeWorker
    expect(worker.options).toEqual({ type: 'module' })
    const encoded = worker.url.replace('data:text/javascript;base64,', '')
    const decoded = new TextDecoder().decode(Uint8Array.from(atob(encoded), (char) => char.charCodeAt(0)))
    expect(decoded).toBe(`self.onmessage = null // ${'ñ'.repeat(40_000)}`)
  })
})

/**
 * Covers {@link renderPdf} (ADR-0018).
 *
 * Scenarios:
 * - the document is opened by ranges read through the parent, with forms, XFA and WASM off, and a worker already started is kept
 * - every page gets a frame of its size; pages are drawn once as they come into view, and the bar tells the first page in view
 * - zoom steps in and out, stops at its ends, and fits the width, drawing the pages again
 * - thumbnails are drawn once, when first shown, and jump to their page
 * - the search finds the pages with the words, walks them, says when nothing matches, and ignores an empty query and other keys
 * - a protected document asks for its password inside the page, says when it is wrong and asks again, ignores an empty one, opens with
 *   the right one and leaves no prompt behind; other failures are thrown as they are
 */
describe('renderPdf', () => {
  beforeEach(() => {
    vi.stubGlobal('Worker', FakeWorker)
    observers.length = 0
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observed = new Set<Element>()
        callback: IntersectionObserverCallback
        constructor(callback: IntersectionObserverCallback) {
          this.callback = callback
          observers.push(this)
        }
        observe(element: Element): void {
          this.observed.add(element)
        }
        unobserve(element: Element): void {
          this.observed.delete(element)
        }
      },
    )
  })

  it('keeps a worker already started', async () => {
    const started = { port: true }
    fakes.GlobalWorkerOptions.workerPort = started
    await open(['one'])
    expect(fakes.GlobalWorkerOptions.workerPort).toBe(started)
    fakes.GlobalWorkerOptions.workerPort = null
  })

  it('opens by ranges and draws the pages in view', async () => {
    const { root, pages, read, observer } = await open(['one', 'two', 'three'])
    const options = fakes.getDocument.mock.calls[0][0] as { range: InstanceType<typeof fakes.PDFDataRangeTransport>; enableXfa: boolean; useWasm: boolean }
    expect(options).toMatchObject({ enableXfa: false, useWasm: false })
    expect(options.range.length).toBe(1000)
    options.range.requestDataRange(100, 200)
    await settle()
    expect(read).toHaveBeenCalledWith(100, 200)
    expect(options.range.onDataRange).toHaveBeenCalledWith(100, expect.any(Uint8Array))

    const frames = root.querySelectorAll('.pdf-page')
    expect(frames).toHaveLength(3)
    expect((frames[0] as HTMLElement).style.width).toBe('750px')
    expect(root.querySelector('.pdf-position')?.textContent).toBe('Page 1 of 3')
    see(observer, root, [2, 3], [1])
    see(observer, root, [2])
    expect(pages[1].render).toHaveBeenCalledTimes(1)
    expect(root.querySelector('.pdf-position')?.textContent).toBe('Page 2 of 3')
    see(observer, root, [], [2])
    expect(root.querySelector('.pdf-position')?.textContent).toBe('Page 2 of 3')
  })

  it('zooms, fits the width and shows thumbnails', async () => {
    const { root, pages, observer } = await open(['one', 'two'])
    const button = (name: string) => [...root.querySelectorAll('button')].find((node) => node.textContent === name || node.getAttribute('aria-label') === name) as HTMLButtonElement
    button('Zoom in').click()
    expect(root.querySelector('.pdf-zoom')?.textContent).toBe('150%')
    for (let step = 0; step < 5; step++) button('Zoom in').click()
    expect(root.querySelector('.pdf-zoom')?.textContent).toBe('300%')
    for (let step = 0; step < 8; step++) button('Zoom out').click()
    expect(root.querySelector('.pdf-zoom')?.textContent).toBe('50%')
    Object.defineProperty(root.querySelector('.pdf-pages'), 'clientWidth', { value: 648 })
    button('Fit width').click()
    expect(root.querySelector('.pdf-zoom')?.textContent).toBe('100%')
    expect(observer.observed.size).toBe(2)

    const thumbs = root.querySelector('.pdf-thumbs') as HTMLElement
    button('Pages').click()
    expect(thumbs.hidden).toBe(false)
    expect(thumbs.querySelectorAll('.pdf-thumb')).toHaveLength(2)
    expect(pages[0].render).toHaveBeenCalled()
    ;(thumbs.querySelectorAll('.pdf-thumb')[1] as HTMLButtonElement).click()
    button('Pages').click()
    button('Pages').click()
    expect(thumbs.querySelectorAll('.pdf-thumb')).toHaveLength(2)
  })

  it('searches the text', async () => {
    const { root, pages } = await open(['alpha', 'the rare quetzal', 'Quetzal again'])
    const search = root.querySelector('.pdf-search') as HTMLInputElement
    const press = async (key: string) => {
      search.dispatchEvent(new KeyboardEvent('keydown', { key }))
      await settle()
    }
    search.value = 'quetzal'
    await press('Enter')
    expect(root.querySelector('.pdf-found')?.textContent).toBe('Found on page 2 · 2 pages')
    await press('Enter')
    expect(root.querySelector('.pdf-found')?.textContent).toBe('Found on page 3 · 2 pages')
    expect(pages[0].getTextContent).toHaveBeenCalledTimes(1)
    search.value = 'missing'
    await press('Enter')
    expect(root.querySelector('.pdf-found')?.textContent).toBe('Not found')
    search.value = '  '
    await press('Enter')
    expect(root.querySelector('.pdf-found')?.textContent).toBe('')
    await press('a')
  })

  it('asks for the password of a protected document inside the page', async () => {
    const root = document.createElement('main')
    document.body.append(root)
    let finish: (doc: unknown) => void = () => undefined
    const task = { onPassword: undefined as undefined | ((answer: (password: string) => void, reason: number) => void), promise: new Promise((resolve) => (finish = resolve)) }
    fakes.getDocument.mockReturnValueOnce(task)
    const opening = renderPdf(new Uint8Array(1), 1, vi.fn(), root, labels)
    await vi.waitFor(() => expect(task.onPassword).toBeDefined())
    const answers: string[] = []
    const onPassword = task.onPassword as (answer: (password: string) => void, reason: number) => void
    onPassword((password) => answers.push(password), 1)
    const input = root.querySelector('.pdf-password input') as HTMLInputElement
    expect(root.querySelector('.pdf-password')?.getAttribute('aria-label')).toBe('Protected')
    expect(document.activeElement).toBe(input)
    expect(root.querySelector('.pdf-password-wrong')).toBeNull()
    ;(root.querySelector('.pdf-password button') as HTMLButtonElement).click()
    expect(answers).toEqual([])
    input.value = 'nope'
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    onPassword((password) => answers.push(password), 2)
    expect(root.querySelectorAll('.pdf-password')).toHaveLength(1)
    expect(root.querySelector('.pdf-password-wrong')?.textContent).toBe('Wrong')
    const again = root.querySelector('.pdf-password input') as HTMLInputElement
    again.value = 'sentinel'
    ;(root.querySelector('.pdf-password button') as HTMLButtonElement).click()
    expect(answers).toEqual(['nope', 'sentinel'])
    const pages = ['one'].map(page)
    finish({ numPages: 1, getPage: async () => pages[0] })
    await opening
    expect(root.querySelector('.pdf-password')).toBeNull()
    expect(root.querySelector('.pdf-bar')).not.toBeNull()
  })

  it('throws other failures as they are', async () => {
    const root = document.createElement('main')
    fakes.getDocument.mockReturnValueOnce({ promise: Promise.reject(new Error('Invalid PDF structure')) })
    await expect(renderPdf(new Uint8Array(1), 1, vi.fn(), root, labels)).rejects.toThrow('Invalid PDF structure')
  })
})
