import type { QueryClient } from '@tanstack/react-query'
import type { AnyRouter } from '@tanstack/react-router'

import type { Transport } from '@/lib/transport'

import { sessionKey } from './session'

/**
 * Ends the session on the client once the server says nobody is signed in: it forgets the anti-CSRF token, records the session as
 * gone and opens the login screen, so a cookie that expired while the app was open does not leave every screen failing.
 *
 * Many requests fail at once when that happens; only the first finds a session to end, so later calls do nothing, and neither does
 * an answer given before anyone signed in, such as on a fresh install that still has to be set up.
 */
export function endSession(transport: Transport, queryClient: QueryClient, router: AnyRouter): void {
  if (!queryClient.getQueryData(sessionKey)) {
    return
  }
  transport.setCsrfToken(undefined)
  queryClient.setQueryData(sessionKey, null)
  void router.navigate({ to: '/login', replace: true })
}

/**
 * Makes every `unauthenticated` answer of the transport end the session (see {@link endSession}).
 *
 * @returns A function that removes the handler.
 */
export function bindSessionExpiry(transport: Transport, queryClient: QueryClient, router: AnyRouter): () => void {
  transport.setOnUnauthenticated?.(() => endSession(transport, queryClient, router))
  return () => transport.setOnUnauthenticated?.(undefined)
}
