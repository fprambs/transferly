import type { ReactNode } from 'react'

import { TransportContext } from './context'
import type { Transport } from './types'

/** Props of {@link TransportProvider}. */
export interface TransportProviderProps {
  /** The transport made available to the subtree. */
  transport: Transport
  /** Subtree that can call {@link useTransport}. */
  children: ReactNode
}

/** Makes a {@link Transport} available to every component below it. */
export function TransportProvider({ transport, children }: TransportProviderProps) {
  return <TransportContext.Provider value={transport}>{children}</TransportContext.Provider>
}
