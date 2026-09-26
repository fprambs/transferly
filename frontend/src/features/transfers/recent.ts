import { useEffect, useState } from 'react'

import { isActive, type TransferItem } from './transfers-store'

/** How long, in milliseconds, a transfer that just ended stays on the active tab, so its result is seen. */
export const recentWindowMs = 15_000

/** Tells whether a transfer ended within the window that ends at `now`. */
export function endedRecently(item: TransferItem, now: number): boolean {
  const ended = item.record.finishedAt
  return !isActive(item) && ended !== undefined && now - Date.parse(ended) < recentWindowMs
}

/**
 * Reads the clock, and keeps it fresh every second for as long as some transfer ended recently, which is when the
 * active tab has something to take away.
 *
 * The clock is read here, apart from the components, so they stay free of impure calls.
 */
export function useNow(items: Record<string, TransferItem>): number {
  const [now, setNow] = useState(() => Date.now())
  const ticking = Object.values(items).some((item) => endedRecently(item, now))
  useEffect(() => {
    if (!ticking) {
      return undefined
    }
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [ticking])
  return now
}
