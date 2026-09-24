import { createContext, useContext } from 'react'

import type { Transport } from './types'

/** React context that carries the active {@link Transport}. */
export const TransportContext = createContext<Transport | null>(null)

/**
 * Returns the active {@link Transport}.
 *
 * @throws {Error} When called outside a `TransportProvider`.
 */
export function useTransport(): Transport {
  const transport = useContext(TransportContext)
  if (transport === null) {
    throw new Error('useTransport must be used within a TransportProvider')
  }
  return transport
}
