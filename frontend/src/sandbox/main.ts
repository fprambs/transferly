import { isRenderMessage, type PageMessage, type RenderMessage } from './protocol'

/** The renderers of the kinds the isolated page shows; each stage of D2 that needs the page adds its own (ADR-0018). */
const renderers: Partial<Record<string, (message: RenderMessage, root: HTMLElement) => Promise<void>>> = {}

/** Sends a message to the explorer that embeds the page. */
function tell(message: PageMessage): void {
  window.parent.postMessage(message, '*')
}

window.addEventListener('message', (event) => {
  if (event.source !== window.parent || !isRenderMessage(event.data)) {
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
    () => tell({ type: 'sentinel:failed', reason: 'render' }),
  )
})

tell({ type: 'sentinel:ready' })
