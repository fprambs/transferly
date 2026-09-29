import { useEffect, useState } from 'react'

/** Returns the current instant, refreshed every `everyMs` milliseconds, for texts such as "2 minutes ago" that age while they are shown. */
export function useNow(everyMs: number): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), everyMs)
    return () => clearInterval(timer)
  }, [everyMs])
  return now
}
