import { useQueryClient } from '@tanstack/react-query'
import { useMatch, useNavigate } from '@tanstack/react-router'

import { useApi } from '@/lib/api/use-api'

import { filesPrefix } from '../explorer/files-key'

import { useConnectionState, type SiteRef } from './connection-store'

/**
 * Returns what disconnects the user from a site (4.9b): what it is doing is canceled, the server closes its connections, its cached folders
 * are forgotten, it stays disconnected until the user connects again, and it leaves the address, so a reload does not connect it again (the
 * main pane for the start page, the second pane closes). It resolves to whether the server disconnected it.
 */
export function useDisconnect(): (site: SiteRef) => Promise<boolean> {
  const api = useApi()
  const queryClient = useQueryClient()
  const markDisconnected = useConnectionState((state) => state.markDisconnected)
  const match = useMatch({ from: '/shell/sites/$siteId', shouldThrow: false })
  const navigate = useNavigate()

  return async (site) => {
    const prefix = filesPrefix(site.workspaceId, site.siteId)
    await queryClient.cancelQueries({ queryKey: prefix })
    try {
      await api.sites.disconnect(site.workspaceId, site.siteId)
    } catch {
      return false
    }
    queryClient.removeQueries({ queryKey: prefix })
    markDisconnected(site)
    if (match?.params.siteId === site.siteId) {
      void navigate({ to: '/' })
    } else if (match?.search.split === site.siteId) {
      void navigate({ to: '.', search: (previous: Record<string, unknown>) => ({ ...previous, split: undefined, splitPath: undefined }) })
    }
    return true
  }
}
