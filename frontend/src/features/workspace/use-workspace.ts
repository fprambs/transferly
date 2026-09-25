import { useQuery } from '@tanstack/react-query'

import type { Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** Query key of the workspaces the user belongs to. */
export const workspacesKey = ['workspaces'] as const

/** Picks the workspace to work in: the personal one, or the first when there is none. */
export function pickWorkspace(workspaces: readonly Workspace[]): Workspace | undefined {
  return workspaces.find((workspace) => workspace.kind === 'personal') ?? workspaces[0]
}

/** Returns the workspace in use, or `undefined` while it loads. */
export function useWorkspace(): Workspace | undefined {
  const api = useApi()
  const { data } = useQuery({ queryKey: workspacesKey, queryFn: () => api.workspaces.list() })
  return data === undefined ? undefined : pickWorkspace(data)
}
