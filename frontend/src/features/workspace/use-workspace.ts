import { useQuery } from '@tanstack/react-query'

import type { Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { useUiStore } from '@/state/ui-store'

/** Query key of the workspaces the user belongs to. */
export const workspacesKey = ['workspaces'] as const

/** Picks the workspace to work in: the chosen one if it still exists, else the personal one, else the first. */
export function pickWorkspace(workspaces: readonly Workspace[], chosenId: string | null = null): Workspace | undefined {
  return (
    workspaces.find((workspace) => workspace.id === chosenId) ??
    workspaces.find((workspace) => workspace.kind === 'personal') ??
    workspaces[0]
  )
}

/** Returns every workspace of the user; empty while they load. */
export function useWorkspaces(): Workspace[] {
  const api = useApi()
  const { data } = useQuery({ queryKey: workspacesKey, queryFn: () => api.workspaces.list() })
  return data ?? []
}

/** Returns the workspace in use, or `undefined` while it loads. */
export function useWorkspace(): Workspace | undefined {
  const workspaces = useWorkspaces()
  const chosenId = useUiStore((state) => state.workspaceId)
  return pickWorkspace(workspaces, chosenId)
}
