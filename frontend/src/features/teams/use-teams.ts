import { useQuery } from '@tanstack/react-query'

import type { Invitation, Member } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** Query keys of the team data of a workspace. */
export const teamKeys = {
  /** The members of a workspace. */
  members: (workspaceId: string) => ['workspaces', workspaceId, 'members'] as const,
  /** The invitations waiting in a workspace. */
  invitations: (workspaceId: string) => ['workspaces', workspaceId, 'invitations'] as const,
}

/** Result of a list query: the items, and whether they are still loading. */
export interface TeamList<T> {
  /** The items; empty while they load. */
  items: T[]
  /** Whether the first answer has not arrived. */
  loading: boolean
}

/** Returns the members of a team workspace. It stays idle while `enabled` is false, for personal workspaces. */
export function useMembers(workspaceId: string, enabled: boolean): TeamList<Member> {
  const api = useApi()
  const { data, isPending } = useQuery({ queryKey: teamKeys.members(workspaceId), queryFn: () => api.workspaces.members(workspaceId), enabled })
  return { items: data ?? [], loading: enabled && isPending }
}

/** Returns the invitations waiting in a team workspace. It stays idle while `enabled` is false, for roles that cannot manage members. */
export function useInvitations(workspaceId: string, enabled: boolean): TeamList<Invitation> {
  const api = useApi()
  const { data, isPending } = useQuery({ queryKey: teamKeys.invitations(workspaceId), queryFn: () => api.invitations.list(workspaceId), enabled })
  return { items: data ?? [], loading: enabled && isPending }
}
