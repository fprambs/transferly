import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import { type ThemePreference, themePreferences } from '@/lib/theme'

/** Interface preferences kept across sessions. */
export interface UiState {
  /** Chosen color scheme. */
  theme: ThemePreference
  /** Whether the transfer queue panel is expanded. */
  queueOpen: boolean
  /** Changes the color scheme. */
  setTheme: (theme: ThemePreference) => void
  /** Expands or collapses the transfer queue panel. */
  toggleQueue: () => void
}

/** The subset of {@link UiState} that is persisted. */
type PersistedUi = Pick<UiState, 'theme' | 'queueOpen'>

/**
 * Keeps only the values of a persisted document that have the expected type,
 * because storage content is not validated by the persistence layer.
 */
function sanitize(raw: unknown): Partial<PersistedUi> {
  const value = (raw ?? {}) as Record<string, unknown>
  const clean: Partial<PersistedUi> = {}
  if (themePreferences.includes(value.theme as ThemePreference)) {
    clean.theme = value.theme as ThemePreference
  }
  if (typeof value.queueOpen === 'boolean') {
    clean.queueOpen = value.queueOpen
  }
  return clean
}

/** Store with the persisted interface preferences. */
export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      theme: 'system',
      queueOpen: true,
      setTheme: (theme) => set({ theme }),
      toggleQueue: () => set((state) => ({ queueOpen: !state.queueOpen })),
    }),
    {
      name: 'sentinel-ui',
      partialize: ({ theme, queueOpen }): PersistedUi => ({ theme, queueOpen }),
      merge: (persisted, current) => ({ ...current, ...sanitize(persisted) }),
    },
  ),
)
