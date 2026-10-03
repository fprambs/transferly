import { QueryClient } from '@tanstack/react-query'
import type { AnyRouter } from '@tanstack/react-router'
import { describe, expect, it, vi } from 'vitest'

import type { Transport } from '@/lib/transport'
import { adminSession } from '@/test/handlers'

import { sessionKey } from './session'
import { bindSessionExpiry, endSession } from './session-expired'

/** Builds a transport that keeps its unauthenticated handler, a client holding a session and a router that records navigations. */
function setup() {
  let handler: (() => void) | undefined
  const transport = {
    setCsrfToken: vi.fn(),
    setOnUnauthenticated: vi.fn((next: (() => void) | undefined) => {
      handler = next
    }),
  } as unknown as Transport
  const queryClient = new QueryClient()
  queryClient.setQueryData(sessionKey, adminSession)
  const navigate = vi.fn(() => Promise.resolve())
  const router = { navigate } as unknown as AnyRouter
  return { transport, queryClient, router, navigate, expire: () => handler?.(), bound: () => handler !== undefined }
}

/**
 * Covers {@link endSession} and {@link bindSessionExpiry}.
 *
 * Use cases:
 * - a session that expires while the app is open is forgotten, with its anti-CSRF token, and the login screen opens once
 *   however many requests fail at the same time
 * - after signing in again without reloading, a second expiry sends the user to login again
 * - an answer before anyone signed in, as on a fresh install, changes nothing
 * - unbinding removes the handler, and a transport that does not report expiries is left alone
 */
describe('session expiry', () => {
  it('ends the session once and opens the login screen', () => {
    const { transport, queryClient, router, navigate, expire } = setup()
    bindSessionExpiry(transport, queryClient, router)

    expire()
    expire()

    expect(queryClient.getQueryData(sessionKey)).toBeNull()
    expect(transport.setCsrfToken).toHaveBeenCalledWith(undefined)
    expect(navigate).toHaveBeenCalledTimes(1)
    expect(navigate).toHaveBeenCalledWith({ to: '/login', replace: true })
  })

  it('ends a later session again after signing in without reloading', () => {
    const { transport, queryClient, router, navigate } = setup()

    endSession(transport, queryClient, router)
    queryClient.setQueryData(sessionKey, adminSession)
    endSession(transport, queryClient, router)

    expect(navigate).toHaveBeenCalledTimes(2)
  })

  it('leaves visitors who never signed in alone', () => {
    const { transport, queryClient, router, navigate } = setup()
    queryClient.removeQueries({ queryKey: sessionKey })

    endSession(transport, queryClient, router)

    expect(navigate).not.toHaveBeenCalled()
    expect(transport.setCsrfToken).not.toHaveBeenCalled()
  })

  it('removes its handler and tolerates transports that do not report expiries', () => {
    const { transport, queryClient, router, bound } = setup()
    const unbind = bindSessionExpiry(transport, queryClient, router)
    expect(bound()).toBe(true)

    unbind()

    expect(bound()).toBe(false)
    const silent = { setCsrfToken: vi.fn() } as unknown as Transport
    expect(() => bindSessionExpiry(silent, queryClient, router)()).not.toThrow()
  })
})
