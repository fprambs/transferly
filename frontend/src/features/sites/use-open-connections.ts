import { useQuery } from '@tanstack/react-query'

import type { OpenConnections } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** No site connected, shared so an empty answer does not change identity between renders. */
const none: ReadonlySet<string> = new Set()

/** Query key of the connected sites of a workspace; the event stream keeps it current. */
export function openConnectionsKey(workspaceId: string) {
  return ['workspaces', workspaceId, 'connections'] as const
}

/** Turns the answer of the server into a set of site identifiers. */
function toSet(answer: OpenConnections): ReadonlySet<string> {
  return new Set(answer.siteIds)
}

/** Applies a connection.state change to the connected sites of a workspace, when they were loaded. */
export function withState(answer: OpenConnections | undefined, siteId: string, open: boolean): OpenConnections | undefined {
  if (answer === undefined) {
    return undefined
  }
  const others = answer.siteIds.filter((id) => id !== siteId)
  return { siteIds: open ? [...others, siteId] : others }
}

/** The sites and quick connections of a workspace to which the user has connections open (4.11). */
export function useOpenConnections(workspaceId: string | undefined): ReadonlySet<string> {
  const api = useApi()
  const { data } = useQuery({
    queryKey: openConnectionsKey(workspaceId ?? ''),
    queryFn: () => api.connections.list(workspaceId as string),
    enabled: workspaceId !== undefined,
    select: toSet,
  })
  return data ?? none
}
