import './viewer.css'

import type { RangeReader } from './pdf'
import { isRangeReply, isRenderMessage, type PageMessage, type RenderMessage } from './protocol'

/** Sends a message to the explorer that embeds the page. */
function tell(message: PageMessage): void {
  window.parent.postMessage(message, '*')
}

/** The ranges asked for and not answered yet, by request. */
const waiting = new Map<number, (bytes: ArrayBuffer | null) => void>()

/** The identifier of the next range asked for. */
let nextRange = 0

/** Asks the parent for a range of the file being shown: the page itself has no network. */
const readRange: RangeReader = (begin, end) =>
  new Promise((resolve, reject) => {
    const id = nextRange++
    waiting.set(id, (bytes) => (bytes === null ? reject(new Error('range not read')) : resolve(new Uint8Array(bytes))))
    tell({ type: 'sentinel:range', id, begin, end })
  })

/** The renderers of the kinds the isolated page shows, each loaded only when a file needs it; each stage of D2 that needs the page adds
 * its own (ADR-0018). */
const renderers: Partial<Record<string, (message: RenderMessage, root: HTMLElement) => Promise<void>>> = {
  markdown: async (message, root) => (await import('./markdown')).renderMarkdown(new TextDecoder().decode(message.bytes), root),
  pdf: async (message, root) => (await import('./pdf')).renderPdf(new Uint8Array(message.bytes), message.size, readRange, root, message.labels),
  document: async (message, root) => (await import('./office/document')).renderDocument(new Uint8Array(message.bytes), root),
  spreadsheet: async (message, root) => (await import('./office/spreadsheet')).renderSpreadsheet(new Uint8Array(message.bytes), root, message.labels),
  presentation: async (message, root) => (await import('./office/presentation')).renderPresentation(new Uint8Array(message.bytes), root, message.labels),
}

/** The errors of the renderers that mean the file is protected with a password. */
const protectedErrors = new Set(['ProtectedPdf', 'ProtectedDocument'])

window.addEventListener('message', (event) => {
  if (event.source !== window.parent) {
    return
  }
  if (isRangeReply(event.data)) {
    waiting.get(event.data.id)?.(event.data.bytes)
    waiting.delete(event.data.id)
    return
  }
  if (!isRenderMessage(event.data)) {
    return
  }
  const message = event.data
  for (const [name, value] of Object.entries(message.theme)) {
    document.documentElement.style.setProperty(name, value)
  }
  const root = document.getElementById('viewer') as HTMLElement
  root.replaceChildren()
  const render = renderers[message.kind]
  if (render === undefined) {
    tell({ type: 'sentinel:failed', reason: 'unsupported' })
    return
  }
  render(message, root).then(
    () => tell({ type: 'sentinel:rendered' }),
    (error: unknown) => tell({ type: 'sentinel:failed', reason: error instanceof Error && protectedErrors.has(error.name) ? 'password' : 'render' }),
  )
})

document.addEventListener(
  'click',
  (event) => {
    if (event.target instanceof Element && event.target.closest('a') !== null) {
      event.preventDefault()
    }
  },
  true,
)

document.addEventListener('keydown', (event) => {
  const typing = event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement
  const walks = (event.key === 'ArrowLeft' || event.key === 'ArrowRight') && !typing && !event.altKey && !event.ctrlKey && !event.metaKey
  if (event.key === 'Escape' || walks) {
    tell({ type: 'sentinel:key', key: event.key })
  }
})

tell({ type: 'sentinel:ready' })
