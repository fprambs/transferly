import { useQuery } from '@tanstack/react-query'

import { sessionQuery } from '@/app/session'
import type { SessionInfo } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { useTransport } from '@/lib/transport'

/** Returns the signed-in session, or `null` when nobody is signed in or it is still loading. */
export function useSession(): SessionInfo | null {
  const api = useApi()
  const transport = useTransport()
  const { data } = useQuery(sessionQuery(api, transport))
  return data ?? null
}
