import { queryOptions } from '@tanstack/react-query'

import type { Api } from '@/lib/api/use-api'
import { ApiError, type Transport } from '@/lib/transport'

/** Query key of the signed-in session. */
export const sessionKey = ['session'] as const

/** Query key of the first-run setup status. */
export const setupKey = ['setup-status'] as const

/**
 * Query that resolves the current session, or `null` when nobody is signed in.
 *
 * It keeps the anti-CSRF token of the transport in step with the session.
 */
export function sessionQuery(api: Api, transport: Transport) {
  return queryOptions({
    queryKey: sessionKey,
    staleTime: Infinity,
    retry: false,
    queryFn: async () => {
      try {
        const session = await api.auth.me()
        transport.setCsrfToken(session.csrfToken)
        return session
      } catch (error) {
        if (error instanceof ApiError && error.problem.status === 401) {
          transport.setCsrfToken(undefined)
          return null
        }
        throw error
      }
    },
  })
}

/** Query that tells whether the first administrator still has to be created. */
export function setupQuery(api: Api) {
  return queryOptions({
    queryKey: setupKey,
    staleTime: Infinity,
    retry: false,
    queryFn: async () => (await api.auth.setupStatus()).required,
  })
}
