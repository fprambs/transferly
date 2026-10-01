import { create } from 'zustand'
import { persist } from 'zustand/middleware'

/** One connection tab (4.12): the site it shows and where the user was in it. */
export interface ConnectionTab {
  /** Identifier of the tab. */
  id: string
  /** Workspace of the site. */
  workspaceId: string
  /** Saved site or quick connection shown. */
  siteId: string
  /** Folder in view. */
  path: string
  /** Site of the second pane, when the view is split. */
  split?: string
  /** Folder in view in the second pane. */
  splitPath?: string
  /** Path of the entry selected in the main pane. */
  selection?: string
}

/** Where a tab is: the fields of the address of the site view. */
export type TabPlace = Pick<ConnectionTab, 'workspaceId' | 'siteId' | 'path' | 'split' | 'splitPath'>

/** The open tabs and which one is active, kept per browser. */
interface TabsState {
  /** Open tabs, in their order. */
  tabs: ConnectionTab[]
  /** Identifier of the active tab, or `null` when none is. */
  activeId: string | null
  /** Makes the active tab show a place: the active tab of the workspace follows it, a tab that already shows the site becomes active, and
   * otherwise a tab is opened for it. */
  follow: (place: TabPlace) => void
  /** Opens a tab for a site at a folder, the root by default, or activates the one that shows it, and returns it. */
  openTab: (workspaceId: string, siteId: string, path?: string) => ConnectionTab
  /** Sends the tabs that show a site, in either pane, back to a folder and forgets their selection: a disconnected site starts over (4.14i). */
  restart: (workspaceId: string, siteId: string, path: string) => void
  /** Activates a tab. */
  activate: (id: string) => void
  /** Closes a tab and returns the tab that becomes active, if any. */
  close: (id: string) => ConnectionTab | undefined
  /** Remembers the entry selected in a tab. */
  select: (id: string, selection: string) => void
  /** Makes no tab active, as when the user leaves the sites. */
  deactivate: () => void
}

/** Makes a tab identifier. */
function newId(): string {
  return `tab-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

/** The tab of a workspace that shows a site. */
function tabOf(tabs: readonly ConnectionTab[], workspaceId: string, siteId: string): ConnectionTab | undefined {
  return tabs.find((tab) => tab.workspaceId === workspaceId && tab.siteId === siteId)
}

/** Keeps only the tabs of a stored document that have the expected shape, since storage content is not validated. */
function sanitize(raw: unknown): Partial<Pick<TabsState, 'tabs' | 'activeId'>> {
  const value = (raw ?? {}) as { tabs?: unknown; activeId?: unknown }
  const tabs = Array.isArray(value.tabs)
    ? (value.tabs as Array<Record<string, unknown>>).filter(
        (tab) => typeof tab.id === 'string' && typeof tab.workspaceId === 'string' && typeof tab.siteId === 'string' && typeof tab.path === 'string',
      )
    : []
  const clean = tabs as unknown as ConnectionTab[]
  return { tabs: clean, activeId: clean.some((tab) => tab.id === value.activeId) ? (value.activeId as string) : null }
}

/** Store of the connection tabs. */
export const useTabs = create<TabsState>()(
  persist(
    (set, get) => ({
      tabs: [],
      activeId: null,
      follow: (place) =>
        set((state) => {
          const active = state.tabs.find((tab) => tab.id === state.activeId && tab.workspaceId === place.workspaceId)
          const other = tabOf(state.tabs, place.workspaceId, place.siteId)
          const target = other !== undefined && other.id !== active?.id && active?.siteId !== place.siteId ? other : active
          if (target === undefined) {
            const tab = { id: newId(), ...place }
            return { tabs: [...state.tabs, tab], activeId: tab.id }
          }
          const moved = target.siteId !== place.siteId
          return {
            tabs: state.tabs.map((tab) => (tab.id === target.id ? { ...tab, ...place, selection: moved ? undefined : tab.selection } : tab)),
            activeId: target.id,
          }
        }),
      openTab: (workspaceId, siteId, path = '/') => {
        const existing = tabOf(get().tabs, workspaceId, siteId)
        const tab = existing ?? { id: newId(), workspaceId, siteId, path }
        set((state) => ({ tabs: existing === undefined ? [...state.tabs, tab] : state.tabs, activeId: tab.id }))
        return tab
      },
      restart: (workspaceId, siteId, path) =>
        set((state) => ({
          tabs: state.tabs.map((tab) => {
            if (tab.workspaceId !== workspaceId) {
              return tab
            }
            const main = tab.siteId === siteId ? { path, selection: undefined } : {}
            return { ...tab, ...main, ...(tab.split === siteId ? { splitPath: path } : {}) }
          }),
        })),
      activate: (id) => set({ activeId: id }),
      close: (id) => {
        const { tabs, activeId } = get()
        const index = tabs.findIndex((tab) => tab.id === id)
        const rest = tabs.filter((tab) => tab.id !== id)
        const next = activeId === id ? (rest[Math.min(index, rest.length - 1)] as ConnectionTab | undefined) : rest.find((tab) => tab.id === activeId)
        set({ tabs: rest, activeId: next?.id ?? null })
        return next
      },
      select: (id, selection) => set((state) => ({ tabs: state.tabs.map((tab) => (tab.id === id ? { ...tab, selection } : tab)) })),
      deactivate: () => set({ activeId: null }),
    }),
    {
      name: 'transferly-tabs',
      partialize: ({ tabs, activeId }) => ({ tabs, activeId }),
      merge: (persisted, current) => ({ ...current, ...sanitize(persisted) }),
    },
  ),
)
