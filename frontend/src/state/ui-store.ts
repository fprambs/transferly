import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import { defaultThemeId, findTheme } from '@/lib/theme'

/** What a double click, or Enter, does on a file of the explorer: download it, as FileZilla does, or open it in the remote editor. */
export type FileDoubleClick = 'download' | 'edit' | 'preview'

/** The tabs of the transfer queue panel. */
export type QueueTab = 'active' | 'finished' | 'activity'

/** Interface preferences kept across sessions. */
export interface UiState {
  /** Identifier of the chosen theme. */
  themeId: string
  /** Whether the interface follows the operating system, light by day and dark by night, instead of the chosen theme. */
  followSystem: boolean
  /** Whether the transfer queue panel is expanded. */
  queueOpen: boolean
  /** Tab of the transfer queue that is showing; it is not kept across sessions. */
  queueTab: QueueTab
  /** Whether the transfer queue opened by itself for a transfer, so it closes again once nothing is moving; it is not kept across sessions. */
  queueAuto: boolean
  /** Identifier of the workspace the user chose, or `null` to use the personal one. */
  workspaceId: string | null
  /** What a double click, or Enter, does on a file. */
  fileDoubleClick: FileDoubleClick
  /** Whether this browser has taken its preferences from the account (language, double click), after which the account alone decides them (4.14a). */
  preferencesSynced: boolean
  /** Whether the connection log panel is expanded. */
  logOpen: boolean
  /** Whether the sites panel is shown. */
  sitesOpen: boolean
  /** Whether the details panel beside the files is shown. */
  detailsOpen: boolean
  /** Chooses a theme. */
  setTheme: (themeId: string) => void
  /** Turns following the operating system on or off. */
  setFollowSystem: (followSystem: boolean) => void
  /** Expands or collapses the transfer queue panel. */
  toggleQueue: () => void
  /** Shows one tab of the transfer queue. */
  setQueueTab: (tab: QueueTab) => void
  /** Opens the transfer queue on its active tab so a transfer that just started is seen; an open queue keeps its tab. */
  showActiveTransfers: () => void
  /** Collapses the transfer queue again if it opened by itself; one the user opened stays open. */
  settleQueue: () => void
  /** Chooses the workspace to work in. */
  setWorkspace: (workspaceId: string) => void
  /** Goes back to the personal workspace. */
  clearWorkspace: () => void
  /** Chooses what a double click, or Enter, does on a file. */
  setFileDoubleClick: (action: FileDoubleClick) => void
  /** Records that the preferences now come from the account. */
  markPreferencesSynced: () => void
  /** Expands or collapses the connection log panel. */
  toggleLog: () => void
  /** Expands or collapses the transfer queue panel. */
  setQueueOpen: (open: boolean) => void
  /** Expands or collapses the connection log panel. */
  setLogOpen: (open: boolean) => void
  /** Shows or hides the sites panel. */
  setSitesOpen: (open: boolean) => void
  /** Shows or hides the details panel. */
  setDetailsOpen: (open: boolean) => void
}

/** The subset of {@link UiState} that is persisted. */
type PersistedUi = Pick<UiState, 'themeId' | 'followSystem' | 'queueOpen' | 'workspaceId' | 'fileDoubleClick' | 'preferencesSynced' | 'logOpen' | 'sitesOpen' | 'detailsOpen'>

/** Themes of the first versions, stored as a color scheme, mapped to the themes that replaced them. */
const legacySchemes: Readonly<Record<string, Partial<PersistedUi>>> = {
  light: { themeId: 'transferly-light', followSystem: false },
  dark: { themeId: 'transferly-dark', followSystem: false },
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
  for (const panel of ['logOpen', 'sitesOpen', 'detailsOpen'] as const) {
    if (typeof value[panel] === 'boolean') {
      clean[panel] = value[panel]
    }
  }
  if (typeof value.workspaceId === 'string') {
    clean.workspaceId = value.workspaceId
  }
  if (value.fileDoubleClick === 'download' || value.fileDoubleClick === 'edit') {
    clean.fileDoubleClick = value.fileDoubleClick
  }
  if (typeof value.preferencesSynced === 'boolean') {
    clean.preferencesSynced = value.preferencesSynced
  }
  return clean
}

/** Store with the persisted interface preferences. */
export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      themeId: defaultThemeId,
      followSystem: false,
      queueOpen: false,
      queueTab: 'active',
      queueAuto: false,
      workspaceId: null,
      fileDoubleClick: 'download',
      preferencesSynced: false,
      logOpen: false,
      sitesOpen: true,
      detailsOpen: false,
      setTheme: (themeId) => set({ themeId }),
      setFollowSystem: (followSystem) => set({ followSystem }),
      toggleQueue: () => set((state) => ({ queueOpen: !state.queueOpen, queueAuto: false })),
      setQueueTab: (queueTab) => set({ queueTab }),
      showActiveTransfers: () => set((state) => (state.queueOpen ? {} : { queueOpen: true, queueTab: 'active', queueAuto: true })),
      settleQueue: () => set((state) => (state.queueAuto ? { queueOpen: false, queueAuto: false } : {})),
      setWorkspace: (workspaceId) => set({ workspaceId }),
      clearWorkspace: () => set({ workspaceId: null }),
      setFileDoubleClick: (fileDoubleClick) => set({ fileDoubleClick }),
      markPreferencesSynced: () => set({ preferencesSynced: true }),
      toggleLog: () => set((state) => ({ logOpen: !state.logOpen })),
      setQueueOpen: (queueOpen) => set({ queueOpen, queueAuto: false }),
      setLogOpen: (logOpen) => set({ logOpen }),
      setSitesOpen: (sitesOpen) => set({ sitesOpen }),
      setDetailsOpen: (detailsOpen) => set({ detailsOpen }),
    }),
    {
      name: 'transferly-ui',
      partialize: ({ themeId, followSystem, queueOpen, workspaceId, fileDoubleClick, preferencesSynced, logOpen, sitesOpen, detailsOpen }): PersistedUi => ({
        themeId,
        followSystem,
        queueOpen,
        workspaceId,
        fileDoubleClick,
        preferencesSynced,
        logOpen,
        sitesOpen,
        detailsOpen,
      }),
      merge: (persisted, current) => ({ ...current, ...sanitize(persisted) }),
    },
  ),
)
