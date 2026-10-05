import { Plus, SquareTerminal, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { TerminalView } from './TerminalView'

/** One terminal of the panel: a number that names it and the folder it started in. */
export interface OpenTerminal {
  /** Number of the terminal, which names it. */
  id: number
  /** Folder the shell started in. */
  path: string
}

/** Props of {@link TerminalPanel}. */
export interface TerminalPanelProps {
  /** Workspace of the site. */
  workspaceId: string
  /** Site whose server the shells run on. */
  siteId: string
  /** The terminals, in order. */
  terminals: readonly OpenTerminal[]
  /** Number of the terminal in view. */
  active: number
  /** Called to show a terminal. */
  onActivate: (id: number) => void
  /** Called to open another terminal in the folder in view. */
  onNew: () => void
  /** Called to close a terminal, which ends its shell. */
  onClose: (id: number) => void
  /** Called to close every terminal and the panel. */
  onCloseAll: () => void
}

/**
 * The terminals of a site under its explorer (ADR-0027): a tab for each, named after its number and folder, with a button to open
 * another one in the folder in view and one to close them all. Every terminal stays connected while another is in view.
 */
export function TerminalPanel({ workspaceId, siteId, terminals, active, onActivate, onNew, onClose, onCloseAll }: TerminalPanelProps) {
  const { t } = useTranslation('explorer')
  return (
    <section aria-label={t(($) => $.terminal.label)} className="flex h-full min-h-0 flex-col border-t bg-card">
      <div className="flex h-9 shrink-0 items-center gap-1 border-b px-2">
        <SquareTerminal aria-hidden="true" className="size-4 text-muted-foreground" />
        <div role="tablist" aria-label={t(($) => $.terminal.label)} className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {terminals.map((terminal) => {
            const name = t(($) => $.terminal.tab, { n: terminal.id })
            return (
              <div
                key={terminal.id}
                className={cn('flex h-7 shrink-0 items-center gap-1 rounded-md pe-1 ps-2 text-xs', terminal.id === active ? 'bg-muted text-foreground' : 'text-muted-foreground hover:bg-muted/60')}
              >
                <button type="button" role="tab" aria-selected={terminal.id === active} onClick={() => onActivate(terminal.id)} className="flex items-center gap-1.5">
                  <span className="font-medium">{name}</span>
                  <span className="max-w-40 truncate font-mono text-[11px] opacity-70">{terminal.path}</span>
                </button>
                <button type="button" aria-label={t(($) => $.terminal.close, { name })} onClick={() => onClose(terminal.id)} className="rounded p-0.5 hover:bg-background">
                  <X className="size-3" />
                </button>
              </div>
            )
          })}
        </div>
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.terminal.new)} onClick={onNew}>
          <Plus />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.terminal.closeAll)} onClick={onCloseAll}>
          <X />
        </Button>
      </div>
      <div className="relative min-h-0 flex-1">
        {terminals.map((terminal) => (
          <div key={terminal.id} role="tabpanel" hidden={terminal.id !== active} className="absolute inset-0">
            <TerminalView workspaceId={workspaceId} siteId={siteId} path={terminal.path} />
          </div>
        ))}
      </div>
    </section>
  )
}
