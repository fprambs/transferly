import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import { defaultThemeId, findTheme } from '@/lib/theme'

/** What a double click, or Enter, does on a file of the explorer: download it, as FileZilla does, or open it in the remote editor. */
export type FileDoubleClick = 'download' | 'edit'

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
  /** Identifier of the workspace the user chose, or `null` to use the personal one. */
  workspaceId: string | null
  /** What a double click, or Enter, does on a file. */
  fileDoubleClick: FileDoubleClick
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
  /** Chooses the workspace to work in. */
  setWorkspace: (workspaceId: string) => void
  /** Goes back to the personal workspace. */
  clearWorkspace: () => void
  /** Chooses what a double click, or Enter, does on a file. */
  setFileDoubleClick: (action: FileDoubleClick) => void
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
type PersistedUi = Pick<UiState, 'themeId' | 'followSystem' | 'queueOpen' | 'workspaceId' | 'fileDoubleClick' | 'logOpen' | 'sitesOpen' | 'detailsOpen'>

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
  return clean
}

/** Store with the persisted interface preferences. */
export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      themeId: defaultThemeId,
      followSystem: false,
      queueOpen: true,
      queueTab: 'active',
      workspaceId: null,
      fileDoubleClick: 'download',
      logOpen: true,
      sitesOpen: true,
      detailsOpen: true,
      setTheme: (themeId) => set({ themeId }),
      setFollowSystem: (followSystem) => set({ followSystem }),
      toggleQueue: () => set((state) => ({ queueOpen: !state.queueOpen })),
      setQueueTab: (queueTab) => set({ queueTab }),
      showActiveTransfers: () => set((state) => ({ queueOpen: true, queueTab: state.queueOpen ? state.queueTab : 'active' })),
      setWorkspace: (workspaceId) => set({ workspaceId }),
      clearWorkspace: () => set({ workspaceId: null }),
      setFileDoubleClick: (fileDoubleClick) => set({ fileDoubleClick }),
      toggleLog: () => set((state) => ({ logOpen: !state.logOpen })),
      setQueueOpen: (queueOpen) => set({ queueOpen }),
      setLogOpen: (logOpen) => set({ logOpen }),
      setSitesOpen: (sitesOpen) => set({ sitesOpen }),
      setDetailsOpen: (detailsOpen) => set({ detailsOpen }),
    }),
    {
      name: 'sentinel-ui',
      partialize: ({ themeId, followSystem, queueOpen, workspaceId, fileDoubleClick, logOpen, sitesOpen, detailsOpen }): PersistedUi => ({
        themeId,
        followSystem,
        queueOpen,
        workspaceId,
        fileDoubleClick,
        logOpen,
        sitesOpen,
        detailsOpen,
      }),
      merge: (persisted, current) => ({ ...current, ...sanitize(persisted) }),
    },
  ),
)
