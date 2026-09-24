import { createContext, useContext } from 'react'

import { type Platform, webPlatform } from './types'

/** React context that carries the active {@link Platform}; defaults to the browser build. */
export const PlatformContext = createContext<Platform>(webPlatform)

/** Returns the capabilities of the environment the interface runs in. */
export function usePlatform(): Platform {
  return useContext(PlatformContext)
}
