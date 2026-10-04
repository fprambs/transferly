import { useQuery } from '@tanstack/react-query'

import { useApi } from '@/lib/api/use-api'

/** The query key of the version of the instance. */
export const versionKey = ['version'] as const

/** Returns the version of the instance, such as v0.5.0, or undefined while it is not known. */
export function useVersion(): string | undefined {
  const api = useApi()
  const { data } = useQuery({ queryKey: versionKey, queryFn: () => api.version(), staleTime: Infinity })
  return data?.version
}
