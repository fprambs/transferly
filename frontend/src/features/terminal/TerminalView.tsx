import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { nonceStyles } from '@/lib/csp'
import { useTransport } from '@/lib/transport'

import { homePath } from '../sites/start-path'

/** The colors of the terminal: a dark screen in every theme, as terminals usually are, with the brand orange for the cursor. */
const theme = {
  background: '#101416',
  foreground: '#e6e9eb',
  cursor: '#ff9800',
  selectionBackground: '#3a4449',
  black: '#1c2226',
  red: '#ef6b73',
  green: '#7ed492',
  yellow: '#f5c76b',
  blue: '#6cb6ff',
  magenta: '#d499ff',
  cyan: '#5fd7d7',
  white: '#e6e9eb',
}

/** The ready state of a WebSocket that is open. */
const socketOpen = 1

/** Where a terminal stands: opening, open, ended with a status, or gone without one. */
type TerminalState = { kind: 'opening' } | { kind: 'open' } | { kind: 'exited'; code: number } | { kind: 'failed' }

/** Props of {@link TerminalView}. */
export interface TerminalViewProps {
  /** Workspace of the site. */
  workspaceId: string
  /** Site whose server the shell runs on. */
  siteId: string
  /** Folder the shell starts in; the home path starts it where the server opens sessions. */
  path: string
}

/** The text messages of a terminal that the client reads: how the shell ended. */
interface ExitMessage {
  /** Always exit. */
  type: 'exit'
  /** Exit status of the shell, or -1 when it ended without one. */
  code: number
}

/** Reads a text message of the terminal, or `undefined` when it is not one the client knows. */
function exitOf(data: string): ExitMessage | undefined {
  try {
    const message = JSON.parse(data) as Partial<ExitMessage>
    return message.type === 'exit' && typeof message.code === 'number' ? (message as ExitMessage) : undefined
  } catch {
    return undefined
  }
}

/**
 * An interactive shell on the server of a site (ADR-0027), drawn with xterm.js over the WebSocket of the terminal: what is typed
 * goes as binary messages, the size of the screen follows the panel, and when the shell ends its status is written on the screen.
 * A terminal that could not open, or that dropped, says why it may have happened, centered, and can open again.
 */
export function TerminalView({ workspaceId, siteId, path }: TerminalViewProps) {
  const { t } = useTranslation('explorer')
  const transport = useTransport()
  const host = useRef<HTMLDivElement>(null)
  const [state, setState] = useState<TerminalState>({ kind: 'opening' })
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const term = new Terminal({ cursorBlink: true, fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 13, theme, scrollback: 5000 })
    const fit = new FitAddon()
    term.loadAddon(fit)
    const unwatch = nonceStyles(host.current as HTMLDivElement)
    term.open(host.current as HTMLDivElement)
    fit.fit()
    const socket = transport.socket(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/terminal`, { path: path === homePath ? undefined : path, cols: term.cols, rows: term.rows })
    socket.binaryType = 'arraybuffer'
    let exited = false
    socket.onopen = () => {
      setState({ kind: 'open' })
      term.focus()
    }
    socket.onmessage = (message) => {
      if (typeof message.data !== 'string') {
        term.write(new Uint8Array(message.data as ArrayBuffer))
        return
      }
      const exit = exitOf(message.data)
      if (exit !== undefined) {
        exited = true
        term.write(`\r\n\x1b[2m${t(($) => $.terminal.exited, { code: exit.code })}\x1b[0m\r\n`)
        setState({ kind: 'exited', code: exit.code })
      }
    }
    socket.onclose = () => {
      if (!exited) {
        setState({ kind: 'failed' })
      }
    }
    const encoder = new TextEncoder()
    const typed = term.onData((data) => {
      if (socket.readyState === socketOpen) {
        socket.send(encoder.encode(data))
      }
    })
    const resized = term.onResize(({ cols, rows }) => {
      if (socket.readyState === socketOpen) {
        socket.send(JSON.stringify({ type: 'resize', cols, rows }))
      }
    })
    const observer = new ResizeObserver(() => fit.fit())
    observer.observe(host.current as HTMLDivElement)
    return () => {
      observer.disconnect()
      unwatch()
      typed.dispose()
      resized.dispose()
      socket.onclose = null
      socket.close()
      term.dispose()
    }
  }, [transport, workspaceId, siteId, path, attempt, t])

  return (
    <div className="relative flex h-full min-h-0 flex-col bg-[#101416]">
      <div ref={host} data-testid="terminal-screen" className="min-h-0 flex-1 px-2 py-1" />
      {state.kind === 'opening' && (
        <div className="absolute inset-0 flex items-center justify-center">
          <Loading label={t(($) => $.terminal.opening)} />
        </div>
      )}
      {state.kind === 'failed' && (
        <div role="alert" className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-[#101416]/95 p-6 text-center text-sm text-[#e6e9eb]">
          <p className="font-medium">{t(($) => $.terminal.failed.title)}</p>
          <p className="max-w-md text-xs text-[#9aa5ab]">{t(($) => $.terminal.failed.body)}</p>
          <Button
            size="sm"
            onClick={() => {
              setState({ kind: 'opening' })
              setAttempt((n) => n + 1)
            }}
          >
            {t(($) => $.terminal.failed.retry)}
          </Button>
        </div>
      )}
    </div>
  )
}
