import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { useMatch, useNavigate } from '@tanstack/react-router'
import { CircleStop, PanelBottom, PanelLeft, PanelRight, PanelTop, RefreshCw, Unplug } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Tip } from '@/components/ui/tip'
import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'
import { useUiStore, type UiState } from '@/state/ui-store'

import { filesPrefix } from '../explorer/files-key'
import { localSiteId } from '../local/local-fs'
import { useSitesData } from '../sites/use-sites-data'
import { useWorkspace } from '../workspace/use-workspace'

import { useConnectionState, useDisconnected, type SiteRef } from './connection-store'

/** Props of {@link ToolButton}. */
interface ToolButtonProps {
  /** What the button does, as its accessible name and tooltip. */
  label: string
  /** Icon of the button. */
  icon: typeof RefreshCw
  /** Whether it can be used now. */
  disabled?: boolean
  /** For a switch, whether it is on. */
  pressed?: boolean
  /** Color of the icon while the button can be used. */
  tone: keyof typeof iconTones
  /** Runs the action. */
  onClick: () => void
}

/** Colors of the toolbar icons, so each action is told apart at a glance: go ahead, stop, cut off, or a panel switch. */
const iconTones = {
  accent: 'text-accent-text hover:bg-accent-text/10',
  stop: 'text-destructive hover:bg-destructive/10',
  cut: 'text-warn hover:bg-warn/10',
  panel: 'text-muted-foreground hover:bg-accent-text/10 aria-pressed:bg-accent-text/15 aria-pressed:text-accent-text',
} as const

/** One icon button of the toolbar, colored by what it does, with its name as a tooltip. */
function ToolButton({ label, icon: Icon, disabled = false, pressed, tone, onClick }: ToolButtonProps) {
  return (
    <Tip label={label}>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={label}
        aria-pressed={pressed}
        disabled={disabled}
        onClick={onClick}
        className={cn('disabled:text-muted-foreground', iconTones[tone])}
      >
        <Icon aria-hidden="true" className="size-4" strokeWidth={2.25} />
      </Button>
    </Tip>
  )
}

/** The site the toolbar acts on: the pane in focus when it is one of the remote panes in view, otherwise the main pane. */
function useTarget(): SiteRef | null {
  const workspace = useWorkspace()
  const focused = useConnectionState((state) => state.focused)
  const match = useMatch({ from: '/shell/sites/$siteId', shouldThrow: false })
  if (workspace === undefined || match === undefined) {
    return null
  }
  const shown = [match.params.siteId, match.search.split].filter((id) => id !== undefined && id !== localSiteId)
  if (focused !== null && focused.workspaceId === workspace.id && shown.includes(focused.siteId)) {
    return focused
  }
  return { workspaceId: workspace.id, siteId: match.params.siteId }
}

/**
 * The toolbar above the view, as in FileZilla (4.9b): actions on the connection in focus (refresh its folder, cancel what it is doing,
 * disconnect) and switches for the panels. It says which connection it acts on. Disconnecting takes the site out of the address, so a
 * reload does not connect it again: the main pane leaves for the start page, the second pane closes.
 */
export function Toolbar() {
  const { t } = useTranslation('explorer')
  const target = useTarget()
  const match = useMatch({ from: '/shell/sites/$siteId', shouldThrow: false })
  const navigate = useNavigate()

  const leave = (site: SiteRef): void => {
    if (match?.params.siteId === site.siteId) {
      void navigate({ to: '/' })
    } else {
      void navigate({ to: '.', search: (previous: Record<string, unknown>) => ({ ...previous, split: undefined, splitPath: undefined }) })
    }
  }

  return (
    <div role="toolbar" aria-label={t(($) => $.commands.label)} className="flex h-9 flex-none items-center gap-1 border-b bg-card px-3">
      {target === null ? <ConnectionActions onDisconnected={leave} /> : <ConnectionActions key={`${target.workspaceId}/${target.siteId}`} target={target} onDisconnected={leave} />}
      <Separator orientation="vertical" className="mx-1 h-5" />
      <PanelSwitches />
    </div>
  )
}

/** Props of {@link ConnectionActions}. */
interface ConnectionActionsProps {
  /** The connection acted on; none when no site is open. */
  target?: SiteRef
  /** Called once the user disconnected from the site, to take it out of the view. */
  onDisconnected: (site: SiteRef) => void
}

/** Refresh, cancel and disconnect, for the connection in focus. */
function ConnectionActions({ target, onDisconnected }: ConnectionActionsProps) {
  const { t } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const { sites, quick } = useSitesData(target?.workspaceId)
  const prefix = target === undefined ? ['none'] : filesPrefix(target.workspaceId, target.siteId)
  const busy = useIsFetching({ queryKey: prefix }) > 0
  const disconnected = useDisconnected(target ?? { workspaceId: '', siteId: '' })
  const markDisconnected = useConnectionState((state) => state.markDisconnected)
  const [failed, setFailed] = useState(false)
  const name = [...sites, ...quick].find((connection) => connection.id === target?.siteId)?.name
  const idle = target === undefined || disconnected

  const disconnect = async (site: SiteRef): Promise<void> => {
    setFailed(false)
    await queryClient.cancelQueries({ queryKey: prefix })
    try {
      await api.sites.disconnect(site.workspaceId, site.siteId)
    } catch {
      setFailed(true)
      return
    }
    queryClient.removeQueries({ queryKey: prefix })
    markDisconnected(site)
    onDisconnected(site)
  }

  return (
    <>
      <ToolButton label={t(($) => $.commands.refresh)} icon={RefreshCw} tone="accent" disabled={idle} onClick={() => void queryClient.invalidateQueries({ queryKey: prefix })} />
      <ToolButton label={t(($) => $.commands.cancel)} icon={CircleStop} tone="stop" disabled={!busy} onClick={() => void queryClient.cancelQueries({ queryKey: prefix })} />
      <ToolButton label={t(($) => $.commands.disconnect)} icon={Unplug} tone="cut" disabled={idle} onClick={() => void disconnect(target as SiteRef)} />
      {name !== undefined && (
        <span className="ms-1 max-w-56 truncate text-xs text-muted-foreground">
          {disconnected ? t(($) => $.commands.disconnectedFrom, { name }) : t(($) => $.commands.connection, { name })}
        </span>
      )}
      {failed && (
        <span role="alert" className="ms-2 text-xs text-destructive">
          {t(($) => $.commands.disconnectFailed)}
        </span>
      )}
    </>
  )
}

/** The panels the toolbar shows or hides, with their preference, its setter and their icon. */
const panelSwitches = [
  { key: 'sites', icon: PanelLeft, open: (state: UiState) => state.sitesOpen, set: (state: UiState) => state.setSitesOpen },
  { key: 'log', icon: PanelTop, open: (state: UiState) => state.logOpen, set: (state: UiState) => state.setLogOpen },
  { key: 'details', icon: PanelRight, open: (state: UiState) => state.detailsOpen, set: (state: UiState) => state.setDetailsOpen },
  { key: 'transfers', icon: PanelBottom, open: (state: UiState) => state.queueOpen, set: (state: UiState) => state.setQueueOpen },
] as const

/** Switches that show or hide the panels around the files (4.10). */
function PanelSwitches() {
  const { t } = useTranslation('explorer')
  const ui = useUiStore()

  return (
    <>
      {panelSwitches.map((panel) => {
        const open = panel.open(ui)
        return <ToolButton key={panel.key} label={t(($) => $.commands.panels[panel.key])} icon={panel.icon} tone="panel" pressed={open} onClick={() => panel.set(ui)(!open)} />
      })}
    </>
  )
}
