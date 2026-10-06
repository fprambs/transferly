import { ChevronDown, ChevronRight, Copy, Eraser } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { BrandIcon } from '@/components/brand/BrandIcon'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useUiStore } from '@/state/ui-store'

import { logKey, useConnectionLog, type LogLine } from './connection-log-store'
import { messageOf } from './messages'

/** Lines kept for a site that has none yet; stable, so the store selector does not rerender for nothing. */
const noLines: LogLine[] = []

/** Formats the time of a line as the clock of the browser shows it, to the second. */
function clockOf(time: string, language: string): string {
  return new Date(time).toLocaleTimeString(language, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
}

/** Steps that tell something went right: connected, a trusted key, signed in, listed. */
const successSteps: readonly string[] = ['connected', 'host_key_verified', 'authenticated', 'elevated', 'listed']

/** Steps that close the session or stop an attempt without an error. */
const quietSteps: readonly string[] = ['disconnected', 'canceled']

/** The color of a line: its level when it warns or fails, success for the steps that went right, muted when the session ends. */
function toneOf(line: Pick<LogLine, 'level' | 'step'>): string {
  if (line.level === 'error') {
    return 'text-destructive'
  }
  if (line.level === 'warn') {
    return 'text-warn'
  }
  if (successSteps.includes(line.step)) {
    return 'text-ok'
  }
  return quietSteps.includes(line.step) ? 'text-muted-foreground' : 'text-foreground'
}

/** Props of {@link ConnectionLog}. */
interface ConnectionLogProps {
  /** Workspace of the site. */
  workspaceId: string
  /** Site whose log is shown. */
  siteId: string
}

/**
 * The connection log of a site, above its files as in FileZilla (ADR-0011): each step the server tells, with its time, colored by how it
 * reads, following the newest line unless the user scrolled up. It can be collapsed, copied as text and cleared; collapsed, it is a status
 * bar with the newest line, red when it is an error, that opens the log when clicked.
 */
export function ConnectionLog({ workspaceId, siteId }: ConnectionLogProps) {
  const { t, i18n } = useTranslation('explorer')
  const lines = useConnectionLog((state) => state.lines[logKey(workspaceId, siteId)] ?? noLines)
  const clear = useConnectionLog((state) => state.clear)
  const open = useUiStore((state) => state.logOpen)
  const toggle = useUiStore((state) => state.toggleLog)
  const scroller = useRef<HTMLOListElement>(null)
  const following = useRef(true)
  const [copied, setCopied] = useState(false)
  const last = lines.at(-1)

  useEffect(() => {
    const list = scroller.current
    if (list !== null && following.current) {
      list.scrollTop = list.scrollHeight
    }
  }, [lines, open])

  const copy = async (): Promise<void> => {
    await navigator.clipboard.writeText(lines.map((line) => `${clockOf(line.time, i18n.language)}  ${messageOf(line, t)}`).join('\n'))
    setCopied(true)
  }

  return (
    <section aria-label={t(($) => $.log.title)} className="flex h-full min-h-0 flex-col bg-surface/40">
      <div className="flex h-8 shrink-0 items-center gap-1 px-2 text-xs">
        <Button variant="ghost" size="xs" aria-expanded={open} onClick={toggle} className="gap-1.5 font-medium">
          {open ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
          <BrandIcon name="logs" />
          {t(($) => $.log.title)}
          <span className="tabular-nums text-muted-foreground group-aria-expanded/button:text-foreground">{lines.length}</span>
        </Button>
        {!open && last !== undefined ? (
          <button
            type="button"
            onClick={toggle}
            title={t(($) => $.log.expand)}
            className={cn('flex min-w-0 flex-1 items-center gap-3 rounded px-1.5 py-0.5 text-start font-mono text-[11px] hover:bg-accent', toneOf(last))}
          >
            <time dateTime={last.time} className="shrink-0 text-faint tabular-nums">
              {clockOf(last.time, i18n.language)}
            </time>
            <span className="truncate">{messageOf(last, t)}</span>
          </button>
        ) : (
          <span className="flex-1" />
        )}
        {copied && (
          <span role="status" className="text-muted-foreground">
            {t(($) => $.log.copied)}
          </span>
        )}
        <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.log.copy)} disabled={lines.length === 0} onClick={() => void copy()}>
          <Copy aria-hidden="true" />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={t(($) => $.log.clear)}
          disabled={lines.length === 0}
          onClick={() => {
            clear(workspaceId, siteId)
            setCopied(false)
          }}
        >
          <Eraser aria-hidden="true" />
        </Button>
      </div>
      {open && (
        <ol
          ref={scroller}
          tabIndex={0}
          aria-label={t(($) => $.log.lines)}
          onScroll={(event) => {
            const list = event.currentTarget
            following.current = list.scrollHeight - list.scrollTop - list.clientHeight < 4
          }}
          className="min-h-0 flex-1 overflow-y-auto px-3 pb-1.5 font-mono text-[11px] leading-5"
        >
          {lines.length === 0 ? (
            <li className="text-muted-foreground">{t(($) => $.log.empty)}</li>
          ) : (
            lines.map((line) => (
              <li key={line.seq} className={cn('flex gap-3', toneOf(line))}>
                <time dateTime={line.time} className="shrink-0 text-faint tabular-nums">
                  {clockOf(line.time, i18n.language)}
                </time>
                <span className="min-w-0 break-all">{messageOf(line, t)}</span>
              </li>
            ))
          )}
        </ol>
      )}
    </section>
  )
}
