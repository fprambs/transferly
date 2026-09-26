import { resolveTimeZone } from '@/lib/datetime'

import { useSession } from './use-session'

/** The IANA time zone times are shown in: the account's, or the browser's while nobody is known. */
export function useTimeZone(): string {
  return useSession()?.user.timezone ?? resolveTimeZone()
}
