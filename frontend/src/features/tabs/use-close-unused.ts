import { useQueryClient } from '@tanstack/react-query'

import { useApi } from '@/lib/api/use-api'

import { filesPrefix } from '../explorer/files-key'
import { localSiteId } from '../local/local-fs'

import { useTabs } from './tabs-store'
import { useConnectionState } from '../toolbar/connection-store'
import { openConnectionsKey, withState } from '../sites/use-open-connections'
import type { OpenConnections } from '@/lib/api'

/**
 * Returns what closes the connections of sites that no tab shows any more, in either pane: a tab that closed, or a site the active tab
 * replaced (4.11, 4.12). The tab named by `exceptTabId`, the one about to show something else, does not count. Their cached folders are
 * forgotten, and a transfer running on a connection finishes before the server closes it. The local folder has no connection.
 */
export function useCloseUnused(): (workspaceId: string, siteIds: ReadonlyArray<string | undefined>, exceptTabId?: string) => void {
  const api = useApi()
  const queryClient = useQueryClient()
  return (workspaceId, siteIds, exceptTabId) => {
    const tabs = useTabs.getState().tabs.filter((tab) => tab.id !== exceptTabId && tab.workspaceId === workspaceId)
    for (const siteId of new Set(siteIds)) {
      if (siteId === undefined || siteId === localSiteId || tabs.some((tab) => tab.siteId === siteId || tab.split === siteId)) {
        continue
      }
      const prefix = filesPrefix(workspaceId, siteId)
      useConnectionState.getState().markDisconnected({ workspaceId, siteId })
      void queryClient.cancelQueries({ predicate: (query) => query.queryKey[0] === 'workspaces' && query.queryKey[1] === workspaceId && query.queryKey.includes(siteId) }).then(async () => {
        await api.sites.disconnect(workspaceId, siteId)
        queryClient.removeQueries({ queryKey: prefix })
        queryClient.setQueryData(openConnectionsKey(workspaceId), (answer: OpenConnections | undefined) => withState(answer, siteId, false))
      }).catch(() => { void queryClient.invalidateQueries({ queryKey: openConnectionsKey(workspaceId) }) })
    }
  }
}
