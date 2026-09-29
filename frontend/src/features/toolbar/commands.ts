import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { useMatch } from '@tanstack/react-router'
import { PanelBottom, PanelLeft, PanelRight, PanelTop } from 'lucide-react'

import type { UiState } from '@/state/ui-store'

import { filesPrefix } from '../explorer/files-key'
import { localSiteId } from '../local/local-fs'
import { useSitesData } from '../sites/use-sites-data'
import { useWorkspace } from '../workspace/use-workspace'

import { useDisconnected, useConnectionState, type SiteRef } from './connection-store'
import { useDisconnect } from './use-disconnect'

/** The site the commands act on: the pane in focus when it is one of the remote panes in view, otherwise the main pane. */
export function useCommandTarget(): SiteRef | null {
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

/** What can be done to the connection the commands act on, shared by the toolbar and the command palette (4.9b, 4.14c). */
export interface ConnectionCommands {
  /** Name of the connection, when it is known. */
  name?: string
  /** Whether it is doing something that can be canceled. */
  busy: boolean
  /** Whether the user disconnected it. */
  disconnected: boolean
  /** Whether there is nothing to refresh or disconnect: no site open, or one disconnected. */
  idle: boolean
  /** Lists the folder in view again. */
  refresh: () => void
  /** Stops what the connection is doing. */
  cancel: () => void
  /** Disconnects it; resolves to whether the server did. */
  disconnect: () => Promise<boolean>
}

/** The commands on a connection, or on none when no site is open. */
export function useConnectionCommands(target: SiteRef | undefined): ConnectionCommands {
  const queryClient = useQueryClient()
  const { sites, quick } = useSitesData(target?.workspaceId)
  const prefix = target === undefined ? ['none'] : filesPrefix(target.workspaceId, target.siteId)
  const busy = useIsFetching({ queryKey: prefix }) > 0
  const disconnected = useDisconnected(target ?? { workspaceId: '', siteId: '' })
  const disconnectFrom = useDisconnect()
  return {
    name: [...sites, ...quick].find((connection) => connection.id === target?.siteId)?.name,
    busy,
    disconnected,
    idle: target === undefined || disconnected,
    refresh: () => void queryClient.invalidateQueries({ queryKey: prefix }),
    cancel: () => void queryClient.cancelQueries({ queryKey: prefix }),
    disconnect: () => disconnectFrom(target as SiteRef),
  }
}

/** The panels that can be shown or hidden, with their preference, its setter, their icon and the shortcut of the ones that have one. */
export const panelSwitches = [
  { key: 'sites', icon: PanelLeft, open: (state: UiState) => state.sitesOpen, set: (state: UiState) => state.setSitesOpen, shortcut: 'Mod+B' },
  { key: 'log', icon: PanelTop, open: (state: UiState) => state.logOpen, set: (state: UiState) => state.setLogOpen },
  { key: 'details', icon: PanelRight, open: (state: UiState) => state.detailsOpen, set: (state: UiState) => state.setDetailsOpen },
  { key: 'transfers', icon: PanelBottom, open: (state: UiState) => state.queueOpen, set: (state: UiState) => state.setQueueOpen },
] as const
