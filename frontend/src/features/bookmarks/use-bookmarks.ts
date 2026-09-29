import { useQuery } from '@tanstack/react-query'

import { useApi } from '@/lib/api/use-api'

/** Query key of the bookmarks of a saved site. */
export function bookmarksKey(workspaceId: string, siteId: string) {
  return ['workspaces', workspaceId, 'sites', siteId, 'bookmarks'] as const
}

/** The bookmarks of a saved site, by name (4.14b); nothing is asked while `enabled` is false, as for a quick connection. */
export function useBookmarks(workspaceId: string, siteId: string, enabled = true) {
  const api = useApi()
  return useQuery({ queryKey: bookmarksKey(workspaceId, siteId), queryFn: () => api.bookmarks.list(workspaceId, siteId), enabled })
}
