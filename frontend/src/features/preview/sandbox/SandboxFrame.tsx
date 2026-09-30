import { useEffect, useRef } from 'react'

import { isPageMessage } from '@/sandbox/protocol'

import type { PreviewSource } from '../source'

/** The theme tokens handed to the isolated page, so what it draws matches the interface. */
const themeTokens = ['--background', '--foreground', '--card', '--muted', '--muted-foreground', '--border', '--accent-text', '--ok', '--info', '--warn']

/** Reads the current value of the theme tokens from the document. */
function currentTheme(): Record<string, string> {
  const style = getComputedStyle(document.documentElement)
  return Object.fromEntries(themeTokens.map((token) => [token, style.getPropertyValue(token).trim()]))
}

/** The most bytes the page may ask for at once; pdf.js asks for chunks far smaller. */
const maxRange = 4 * 1024 * 1024

/** Props of {@link SandboxFrame}. */
export interface SandboxFrameProps {
  /** The kind the page renders the file as. */
  kind: string
  /** Name of the file. */
  name: string
  /** The first bytes of the file, or all of them. */
  bytes: Uint8Array
  /** The file, which serves the ranges the page asks for; left out, the page gets only the bytes above. */
  source?: PreviewSource
  /** The words the renderer shows, already translated. */
  labels?: Record<string, string>
  /** Title of the frame, read to assistive technology. */
  title: string
  /** Called when the page cannot render the file, with its short reason, so the viewer can fall back or explain. */
  onFailed: (reason: string) => void
}

/**
 * Renders a file inside the isolated page of the viewers (ADR-0018): an `iframe` with `sandbox="allow-scripts"`, without
 * `allow-same-origin`, so the page has an opaque origin, no cookies, no API and, by its own policy, no network. Once the page says it is
 * ready, it gets a copy of the first bytes, the size of the file, the colors of the theme and its translated words; a renderer that needs
 * more asks for ranges, which the parent reads from the file, up to 4 MiB at a time, and hands back. Keys the viewer handles, pressed while
 * the focus is inside the page, are pressed again on the frame, so Escape and the arrows keep working. Only messages whose source is that
 * frame are listened to.
 */
export function SandboxFrame({ kind, name, bytes, source, labels = {}, title, onFailed }: SandboxFrameProps) {
  const frame = useRef<HTMLIFrameElement>(null)
  useEffect(() => {
    const onMessage = (event: MessageEvent): void => {
      const page = frame.current?.contentWindow
      if (page === null || page === undefined || event.source !== page || !isPageMessage(event.data)) {
        return
      }
      const message = event.data
      if (message.type === 'sentinel:ready') {
        const copy = bytes.slice().buffer
        page.postMessage({ type: 'sentinel:render', kind, name, bytes: copy, size: source?.size ?? bytes.length, theme: currentTheme(), labels }, '*', [copy])
      } else if (message.type === 'sentinel:failed') {
        onFailed(message.reason ?? '')
      } else if (message.type === 'sentinel:key') {
        frame.current?.dispatchEvent(new KeyboardEvent('keydown', { key: message.key, bubbles: true }))
      } else if (message.type === 'sentinel:range') {
        const reply = (answer: ArrayBuffer | null): void => page.postMessage({ type: 'sentinel:bytes', id: message.id, bytes: answer }, '*', answer === null ? [] : [answer])
        if (source === undefined || message.end - message.begin > maxRange) {
          reply(null)
          return
        }
        source.read(message.begin, message.end).then(
          (chunk) => reply(chunk.slice().buffer),
          () => reply(null),
        )
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [kind, name, bytes, source, labels, onFailed])
  return <iframe ref={frame} title={title} src="/sandbox/viewer.html" sandbox="allow-scripts" className="min-h-0 w-full flex-1 border-0 bg-card" />
}
