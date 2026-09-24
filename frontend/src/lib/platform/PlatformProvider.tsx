import type { ReactNode } from 'react'

import { PlatformContext } from './context'
import type { Platform } from './types'

/** Props of {@link PlatformProvider}. */
export interface PlatformProviderProps {
  /** The capabilities made available to the subtree. */
  platform: Platform
  /** Subtree that can call `usePlatform`. */
  children: ReactNode
}

/** Overrides the platform capabilities for every component below it. */
export function PlatformProvider({ platform, children }: PlatformProviderProps) {
  return <PlatformContext.Provider value={platform}>{children}</PlatformContext.Provider>
}
