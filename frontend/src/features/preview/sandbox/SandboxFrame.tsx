import { useEffect, useRef } from 'react'

import { isPageMessage } from '@/sandbox/protocol'

/** The theme tokens handed to the isolated page, so what it draws matches the interface. */
const themeTokens = ['--background', '--foreground', '--card', '--muted', '--muted-foreground', '--border', '--accent-text', '--ok', '--info', '--warn']

/** Reads the current value of the theme tokens from the document. */
function currentTheme(): Record<string, string> {
  const style = getComputedStyle(document.documentElement)
  return Object.fromEntries(themeTokens.map((token) => [token, style.getPropertyValue(token).trim()]))
}

/** Props of {@link SandboxFrame}. */
export interface SandboxFrameProps {
  /** The kind the page renders the file as. */
  kind: string
  /** Name of the file. */
  name: string
  /** The bytes of the file. */
  bytes: Uint8Array
  /** Title of the frame, read to assistive technology. */
  title: string
  /** Called when the page cannot render the file, so the viewer can fall back. */
  onFailed: () => void
}

/**
 * Renders a file inside the isolated page of the viewers (ADR-0018): an `iframe` with `sandbox="allow-scripts"`, without
 * `allow-same-origin`, so the page has an opaque origin, no cookies, no API and, by its own policy, no network. Once the page says it is
 * ready, it gets a copy of the bytes and the colors of the theme; only messages whose source is that frame are listened to.
 */
export function SandboxFrame({ kind, name, bytes, title, onFailed }: SandboxFrameProps) {
  const frame = useRef<HTMLIFrameElement>(null)
  useEffect(() => {
    const onMessage = (event: MessageEvent): void => {
      const page = frame.current?.contentWindow
      if (page === null || page === undefined || event.source !== page || !isPageMessage(event.data)) {
        return
      }
      if (event.data.type === 'sentinel:ready') {
        const copy = bytes.slice().buffer
        page.postMessage({ type: 'sentinel:render', kind, name, bytes: copy, theme: currentTheme() }, '*', [copy])
      } else if (event.data.type === 'sentinel:failed') {
        onFailed()
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [kind, name, bytes, onFailed])
  return <iframe ref={frame} title={title} src="/sandbox/viewer.html" sandbox="allow-scripts" className="min-h-0 w-full flex-1 border-0 bg-card" />
}
