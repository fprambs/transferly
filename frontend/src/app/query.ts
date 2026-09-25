import { QueryClient } from '@tanstack/react-query'

/** Creates the cache of server state; failed calls are not retried automatically. */
export function createQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } })
}
