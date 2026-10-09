import type { QueryClient } from '@tanstack/react-query'
import { uuid } from '@/lib/uuid'

import { useConnectionLog } from '@/features/connlog/connection-log-store'
import { useLocalRoot } from '@/features/local/local-root-store'
import { useNotices } from '@/features/notices/notices-store'
import { usePrompts } from '@/features/prompts/prompts-store'
import { useRecentSites } from '@/features/sites/recent-sites'
import { useTabs } from '@/features/tabs/tabs-store'
import { useConnectionState } from '@/features/toolbar/connection-store'
import { useTransfersStore } from '@/features/transfers/transfers-store'
import { clearUploadSources } from '@/features/transfers/upload-sources'
import { useDialogsStore } from '@/state/dialogs-store'
import { useUiStore } from '@/state/ui-store'

/** Browser marker used to invalidate private persistence in other tabs; it never contains tokens. */
export const sessionChangeKey = 'transferly-session-change'

/** Publishes an identity transition after private resources have been removed. */
export function announceSessionChange(): void {
  localStorage.setItem(sessionChangeKey, uuid())
}

/** Drops persisted tabs when the browser cookie belongs to a different identity on page load. */
export function ensurePrivateOwner(userId: string): void {
  if (localStorage.getItem('transferly-private-owner') !== userId) {
    useTabs.setState({ tabs: [], activeId: null })
    useRecentSites.setState({ recent: {} })
    localStorage.setItem('transferly-private-owner', userId)
  }
}

/** Removes resources belonging to the previous identity while retaining public appearance preferences. */
export function clearPrivateState(queryClient: QueryClient): void {
  queryClient.clear()
  useTransfersStore.getState().reset()
  clearUploadSources()
  useLocalRoot.getState().setRoot(null)
  useTabs.setState({ tabs: [], activeId: null })
  useRecentSites.setState({ recent: {} })
  useConnectionLog.setState({ lines: {}, next: 1 })
  usePrompts.setState({ waiting: [] })
  useConnectionState.setState({ focused: null, disconnected: {} })
  useNotices.setState({ notices: [], next: 1 })
  useDialogsStore.setState({ quickConnectOpen: false, paletteOpen: false, shortcutsOpen: false })
  useUiStore.setState({ workspaceId: null })
}
