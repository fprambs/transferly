import { useEffect, useRef, useSyncExternalStore } from 'react'

import { crossfade } from '@/lib/motion'
import { useUiStore } from '@/state/ui-store'

import { applyTheme, resolveTheme, type ThemeDefinition } from './index'

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

/** Returns the theme the interface wears now, given the stored choice and the operating system. */
export function useActiveTheme(): ThemeDefinition {
  const themeId = useUiStore((state) => state.themeId)
  const followSystem = useUiStore((state) => state.followSystem)
  return resolveTheme({ themeId, followSystem }, usePrefersDark())
}

/** Keeps the document theme in sync with the stored choice and the operating system; a change after the first one crossfades. */
export function useThemeSync(): void {
  const theme = useActiveTheme()
  const applied = useRef(false)

  useEffect(() => {
    const apply = (): void => applyTheme(document.documentElement, theme)
    if (applied.current) {
      crossfade(apply)
    } else {
      applied.current = true
      apply()
    }
  }, [theme])
}
