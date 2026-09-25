import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import { defaultThemeId, findTheme } from '@/lib/theme'

/** Interface preferences kept across sessions. */
export interface UiState {
  /** Identifier of the chosen theme. */
  themeId: string
  /** Whether the interface follows the operating system, light by day and dark by night, instead of the chosen theme. */
  followSystem: boolean
  /** Whether the transfer queue panel is expanded. */
  queueOpen: boolean
  /** Identifier of the workspace the user chose, or `null` to use the personal one. */
  workspaceId: string | null
  /** Chooses a theme. */
  setTheme: (themeId: string) => void
  /** Turns following the operating system on or off. */
  setFollowSystem: (followSystem: boolean) => void
  /** Expands or collapses the transfer queue panel. */
  toggleQueue: () => void
  /** Chooses the workspace to work in. */
  setWorkspace: (workspaceId: string) => void
}

/** The subset of {@link UiState} that is persisted. */
type PersistedUi = Pick<UiState, 'themeId' | 'followSystem' | 'queueOpen' | 'workspaceId'>

/** Themes of the first versions, stored as a color scheme, mapped to the themes that replaced them. */
const legacySchemes: Readonly<Record<string, Partial<PersistedUi>>> = {
  light: { themeId: 'sentinel-light', followSystem: false },
  dark: { themeId: 'sentinel-dark', followSystem: false },
  system: { followSystem: true },
}

/**
 * Keeps only the values of a persisted document that have the expected type,
 * because storage content is not validated by the persistence layer.
 * Documents written by earlier versions, with a color scheme instead of a theme, are upgraded.
 */
function sanitize(raw: unknown): Partial<PersistedUi> {
  const value = (raw ?? {}) as Record<string, unknown>
  const clean: Partial<PersistedUi> = typeof value.theme === 'string' ? { ...legacySchemes[value.theme] } : {}
  if (typeof value.themeId === 'string' && findTheme(value.themeId) !== undefined) {
    clean.themeId = value.themeId
  }
  if (typeof value.followSystem === 'boolean') {
    clean.followSystem = value.followSystem
  }
  if (typeof value.queueOpen === 'boolean') {
    clean.queueOpen = value.queueOpen
  }
  if (typeof value.workspaceId === 'string') {
    clean.workspaceId = value.workspaceId
  }
  return clean
}

/** Store with the persisted interface preferences. */
export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      themeId: defaultThemeId,
      followSystem: false,
      queueOpen: true,
      workspaceId: null,
      setTheme: (themeId) => set({ themeId }),
      setFollowSystem: (followSystem) => set({ followSystem }),
      toggleQueue: () => set((state) => ({ queueOpen: !state.queueOpen })),
      setWorkspace: (workspaceId) => set({ workspaceId }),
    }),
    {
      name: 'sentinel-ui',
      partialize: ({ themeId, followSystem, queueOpen, workspaceId }): PersistedUi => ({
        themeId,
        followSystem,
        queueOpen,
        workspaceId,
      }),
      merge: (persisted, current) => ({ ...current, ...sanitize(persisted) }),
    },
  ),
)
