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

/** A screen's data, whether it is still loading, and whether it failed to load; `refetch` tries again. */
export interface AdminQuery<T> {
  /** The data, once loaded. */
  data: T | undefined
  /** Whether the first load is still going. */
  loading: boolean
  /** The error that failed the load, or `undefined` when it did not fail. */
  error: unknown
  /** Tries the load again. */
  refetch: () => void
}

/** Returns the accounts of the instance, oldest first. */
export function useAdminUsers(): AdminQuery<AdminUser[]> {
  const api = useApi()
  const { data, isPending, error, refetch } = useQuery({ queryKey: adminKeys.users, queryFn: () => api.admin.users() })
  return { data, loading: isPending, error, refetch: () => void refetch() }
}

/** Returns the private networks sites may reach. */
export function useNetworks(): AdminQuery<Networks> {
  const api = useApi()
  const { data, isPending, error, refetch } = useQuery({ queryKey: adminKeys.networks, queryFn: () => api.admin.networks() })
  return { data, loading: isPending, error, refetch: () => void refetch() }
}

/** Returns the caps and quotas of the instance with the values the server started with. */
export function useLimits(): AdminQuery<InstanceLimitsView> {
  const api = useApi()
  const { data, isPending, error, refetch } = useQuery({ queryKey: adminKeys.limits, queryFn: () => api.admin.limits() })
  return { data, loading: isPending, error, refetch: () => void refetch() }
}
