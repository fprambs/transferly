import { useEffect, useSyncExternalStore } from 'react'

import { useUiStore } from '@/state/ui-store'

import { applyTheme, resolveTheme } from './index'

const darkQuery = '(prefers-color-scheme: dark)'

/** Subscribes a listener to changes of the operating system color scheme. */
function subscribe(listener: () => void): () => void {
  const media = window.matchMedia(darkQuery)
  media.addEventListener('change', listener)
  return () => media.removeEventListener('change', listener)
}

/** Reports whether the operating system currently prefers a dark scheme. */
export function usePrefersDark(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(darkQuery).matches,
    () => false,
  )
}

/** Keeps the document color scheme in sync with the stored preference and the operating system. */
export function useThemeSync(): void {
  const preference = useUiStore((state) => state.theme)
  const prefersDark = usePrefersDark()

  useEffect(() => {
    applyTheme(document.documentElement, resolveTheme(preference, prefersDark))
  }, [preference, prefersDark])
}
