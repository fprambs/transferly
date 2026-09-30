import { useEffect, useMemo } from 'react'

/** How long an address outlives the component that let it go, so a component mounted again at once keeps it. */
const graceMs = 1000

/** Revocations waiting, by address. */
const pending = new Map<string, ReturnType<typeof setTimeout>>()

/**
 * Gives a `blob:` address for a blob and revokes it once the component lets it go. The revocation waits a moment and is called off when the
 * same address is taken again: React mounts components twice in development (`StrictMode`), and revoking at once would leave the second
 * mount with an address that no longer works.
 */
export function useObjectUrl(blob: Blob | null): string | null {
  const url = useMemo(() => (blob === null ? null : URL.createObjectURL(blob)), [blob])
  useEffect(() => {
    if (url === null) {
      return undefined
    }
    clearTimeout(pending.get(url))
    pending.delete(url)
    return () => {
      pending.set(
        url,
        setTimeout(() => {
          pending.delete(url)
          URL.revokeObjectURL(url)
        }, graceMs),
      )
    }
  }, [url])
  return url
}
