import { useQuery } from '@tanstack/react-query'

import type { AdminUser, InstanceLimitsView, Networks } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** Query keys of the data of the administration screens. */
export const adminKeys = {
  /** The accounts of the instance. */
  users: ['admin', 'users'] as const,
  /** The private networks sites may reach. */
  networks: ['admin', 'networks'] as const,
  /** The caps and quotas of the instance. */
  limits: ['admin', 'limits'] as const,
  /** The audit trail of the instance, narrowed by an action prefix. */
  audit: (prefix: string) => ['admin', 'audit', prefix] as const,
}

/** Returns the accounts of the instance, oldest first, and whether they are still loading. */
export function useAdminUsers(): { items: AdminUser[]; loading: boolean } {
  const api = useApi()
  const { data, isPending } = useQuery({ queryKey: adminKeys.users, queryFn: () => api.admin.users() })
  return { items: data ?? [], loading: isPending }
}

/** Returns the private networks sites may reach, or `undefined` while they load. */
export function useNetworks(): Networks | undefined {
  const api = useApi()
  return useQuery({ queryKey: adminKeys.networks, queryFn: () => api.admin.networks() }).data
}

/** Returns the caps and quotas of the instance with the values the server started with, or `undefined` while they load. */
export function useLimits(): InstanceLimitsView | undefined {
  const api = useApi()
  return useQuery({ queryKey: adminKeys.limits, queryFn: () => api.admin.limits() }).data
}
