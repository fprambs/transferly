import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

/** How many sites each workspace remembers as recent. */
export const recentLimit = 6

/** The sites opened last in each workspace, newest first, kept in this browser. */
export interface RecentSitesState {
  /** Identifiers of the sites opened last, by workspace. */
  recent: Record<string, string[]>
  /** Puts a site at the head of the recent ones of its workspace. */
  remember: (workspaceId: string, siteId: string) => void
}

/** Store of the recent sites, which the start page offers to open again. */
export const useRecentSites = create<RecentSitesState>()(
  persist(
    (set) => ({
      recent: {},
      remember: (workspaceId, siteId) =>
        set((state) => ({
          recent: { ...state.recent, [workspaceId]: [siteId, ...(state.recent[workspaceId] ?? []).filter((id) => id !== siteId)].slice(0, recentLimit) },
        })),
    }),
    { name: 'transferly-recent-sites', storage: createJSONStorage(() => localStorage) },
  ),
)
