import { useQuery } from '@tanstack/react-query'

import type { Connector } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** Query key of the catalog of connectors and whether the instance offers each. */
export const connectorsKey = ['connectors'] as const

/** The catalog of connectors, whether it is still loading, and the error that failed it; `refetch` tries again. */
export interface ConnectorsQuery {
  /** The connectors, stable ones first, once loaded. */
  data: Connector[] | undefined
  /** Whether the first load is still going. */
  loading: boolean
  /** The error that failed the load, or `null` when it did not fail. */
  error: unknown
  /** Tries the load again. */
  refetch: () => void
}

/** Returns the connectors Transferly is built with and whether the instance offers each (ADR-0019). */
export function useConnectors(): ConnectorsQuery {
  const api = useApi()
  const { data, isPending, error, refetch } = useQuery({ queryKey: connectorsKey, queryFn: () => api.connectors.list() })
  return { data, loading: isPending, error, refetch: () => void refetch() }
}

