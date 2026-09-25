import { useQuery } from '@tanstack/react-query'

import type { Collection, QuickConnection, Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** Query keys of the data behind the sites tree, by workspace. */
export const sitesKeys = {
  collections: (workspaceId: string) => ['workspaces', workspaceId, 'collections'] as const,
  sites: (workspaceId: string) => ['workspaces', workspaceId, 'sites'] as const,
  quick: (workspaceId: string) => ['workspaces', workspaceId, 'quick'] as const,
}

/** The lists the sites tree is built from; each is empty until it loads. */
export interface SitesData {
  /** Collections of the workspace. */
  collections: Collection[]
  /** Saved sites of the workspace. */
  sites: Site[]
  /** Open quick connections. */
  quick: QuickConnection[]
}

/** Loads the collections, sites and quick connections of a workspace. */
export function useSitesData(workspaceId: string | undefined): SitesData {
  const api = useApi()
  const enabled = workspaceId !== undefined
  const id = workspaceId ?? ''
  const collections = useQuery({ queryKey: sitesKeys.collections(id), queryFn: () => api.collections.list(id), enabled })
  const sites = useQuery({ queryKey: sitesKeys.sites(id), queryFn: () => api.sites.list(id), enabled })
  const quick = useQuery({ queryKey: sitesKeys.quick(id), queryFn: () => api.quick.list(id), enabled })
  return { collections: collections.data ?? [], sites: sites.data ?? [], quick: quick.data ?? [] }
}
