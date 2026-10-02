import { ChevronDown, CircleStop, LayoutPanelLeft, RefreshCw, Unplug } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Separator } from '@/components/ui/separator'
import { Tip } from '@/components/ui/tip'
import { cn } from '@/lib/utils'
import { useUiStore } from '@/state/ui-store'

import { TabBar } from '../tabs/TabBar'

import { panelSwitches, useCommandTarget, useConnectionCommands } from './commands'
import type { SiteRef } from './connection-store'

/** Props of {@link ToolButton}. */
interface ToolButtonProps {
  /** What the button does, as its accessible name and tooltip. */
  label: string
  /** Icon of the button. */
  icon: typeof RefreshCw
  /** Whether it can be used now. */
  disabled?: boolean
  /** Color of the icon while the button can be used. */
  tone: keyof typeof iconTones
  /** Runs the action. */
  onClick: () => void
}

/** Colors of the toolbar icons, so each action is told apart at a glance: go ahead, stop or cut off. */
const iconTones = {
  accent: 'text-accent-text hover:bg-accent-text/10',
  stop: 'text-destructive hover:bg-destructive/10',
  cut: 'text-warn hover:bg-warn/10',
} as const

/** One icon button of the toolbar, colored by what it does, with its name as a tooltip. */
function ToolButton({ label, icon: Icon, disabled = false, tone, onClick }: ToolButtonProps) {
  return (
    <Tip label={label}>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={label}
        disabled={disabled}
        onClick={onClick}
        className={cn('disabled:text-muted-foreground', iconTones[tone])}
      >
        <Icon aria-hidden="true" className="size-4" strokeWidth={2.25} />
      </Button>
    </Tip>
  )
}

/**
 * The one bar above the view: the tabs of the open connections on the left and, on the right, the toolbar (4.9b) with the actions on the
 * connection in focus (refresh its folder, cancel what it is doing, disconnect) and the View menu that shows or hides the panels. The
 * connection it acts on is named for screen readers; the tabs name it for everyone. Disconnecting takes the site out of the address, so a
 * reload does not connect it again: the main pane leaves for the start page, the second pane closes.
 */
export function Toolbar() {
  const { t } = useTranslation('explorer')
  const target = useCommandTarget()
  return (
    <div className="flex h-9 flex-none items-end border-b bg-card">
      <TabBar />
      <div role="toolbar" aria-label={t(($) => $.commands.label)} className="ms-auto flex h-full flex-none items-center gap-0.5 ps-2 pe-2">
        {target === null ? <ConnectionActions /> : <ConnectionActions key={`${target.workspaceId}/${target.siteId}`} target={target} />}
        <Separator orientation="vertical" className="mx-1 h-5" />
        <ViewMenu />
      </div>
    </div>
  )
}

/** Props of {@link ConnectionActions}. */
interface ConnectionActionsProps {
  /** The connection acted on; none when no site is open. */
  target?: SiteRef
}

/** Refresh, cancel and disconnect, for the connection in focus. */
function ConnectionActions({ target }: ConnectionActionsProps) {
  const { t } = useTranslation('explorer')
  const { name, busy, disconnected, idle, refresh, cancel, disconnect } = useConnectionCommands(target)
  const [failed, setFailed] = useState(false)

  return (
    <>
      <ToolButton label={t(($) => $.commands.refresh)} icon={RefreshCw} tone="accent" disabled={idle} onClick={refresh} />
      <ToolButton label={t(($) => $.commands.cancel)} icon={CircleStop} tone="stop" disabled={!busy} onClick={cancel} />
      <ToolButton label={t(($) => $.commands.disconnect)} icon={Unplug} tone="cut" disabled={idle} onClick={async () => setFailed(!(await disconnect()))} />
      {name !== undefined && <span className="sr-only">{disconnected ? t(($) => $.commands.disconnectedFrom, { name }) : t(($) => $.commands.connection, { name })}</span>}
      {failed && (
        <span role="alert" className="order-first me-2 text-xs text-destructive">
          {t(($) => $.commands.disconnectFailed)}
        </span>
      )}
    </>
  )
}

/** The View menu: switches that show or hide the panels around the files (4.10), checked while the panel is shown. */
function ViewMenu() {
  const { t } = useTranslation('explorer')
  const ui = useUiStore()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium text-muted-foreground hover:bg-accent hover:text-foreground data-popup-open:bg-accent data-popup-open:text-foreground">
        <LayoutPanelLeft aria-hidden="true" className="size-4" />
        <span className="max-md:sr-only">{t(($) => $.commands.view)}</span>
        <ChevronDown aria-hidden="true" className="size-3" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        {panelSwitches.map((panel) => {
          const open = panel.open(ui)
          const Icon = panel.icon
          return (
            <DropdownMenuCheckboxItem key={panel.key} checked={open} onCheckedChange={(checked) => panel.set(ui)(checked)}>
              <Icon aria-hidden="true" className="text-muted-foreground" />
              {t(($) => $.commands.panels[panel.key])}
            </DropdownMenuCheckboxItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
