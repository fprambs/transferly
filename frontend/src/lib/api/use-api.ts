import { useMemo } from 'react'

import { useTransport } from '../transport'

import { createApi } from './index'

/** The typed client of the backend. */
export type Api = ReturnType<typeof createApi>

/** Returns the typed client bound to the active transport. */
export function useApi(): Api {
  const transport = useTransport()
  return useMemo(() => createApi(transport), [transport])
}
