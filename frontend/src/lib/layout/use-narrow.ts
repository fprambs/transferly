import { useSyncExternalStore } from 'react'

/** Widths where the panels beside the files do not fit: a phone, or a narrow window. */
export const narrowQuery = '(max-width: 767px)'

/** Subscribes a listener to the window crossing the narrow width. */
function subscribe(listener: () => void): () => void {
  const media = window.matchMedia(narrowQuery)
  media.addEventListener('change', listener)
  return () => media.removeEventListener('change', listener)
}

/** Reports whether the window is narrow, so the shell keeps the sites in a sheet and the details hidden and leaves the width to the files. */
export function useNarrow(): boolean {
  return useSyncExternalStore(subscribe, () => window.matchMedia(narrowQuery).matches)
}
