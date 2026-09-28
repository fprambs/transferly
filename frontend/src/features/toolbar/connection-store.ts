import { create } from 'zustand'

import { logKey } from '../connlog/connection-log-store'

/** A site of a workspace. */
export interface SiteRef {
  /** Workspace of the site. */
  workspaceId: string
  /** The site, or quick connection. */
  siteId: string
}

/** Which connection the toolbar acts on, and which ones the user disconnected (4.9b). */
interface ConnectionState {
  /** The pane the user clicked or focused last; `null` before any. */
  focused: SiteRef | null
  /** Sites the user disconnected, keyed by {@link logKey}; they do not connect again until asked to. */
  disconnected: Record<string, true>
  /** Makes a site the connection in focus. */
  focus: (site: SiteRef) => void
  /** Records that the user disconnected from a site. */
  markDisconnected: (site: SiteRef) => void
  /** Lets a disconnected site connect again. */
  reconnect: (site: SiteRef) => void
  /** Lets a site connect again, in whichever workspace it was disconnected, as opening it from the sidebar does. */
  reconnectSite: (siteId: string) => void
}

/** Store of the connection in focus and of the disconnected sites. */
export const useConnectionState = create<ConnectionState>()((set) => ({
  focused: null,
  disconnected: {},
  focus: (site) => set({ focused: site }),
  markDisconnected: (site) => set((state) => ({ disconnected: { ...state.disconnected, [logKey(site.workspaceId, site.siteId)]: true } })),
  reconnect: (site) =>
    set((state) => ({ disconnected: Object.fromEntries(Object.entries(state.disconnected).filter(([key]) => key !== logKey(site.workspaceId, site.siteId))) })),
  reconnectSite: (siteId) => set((state) => ({ disconnected: Object.fromEntries(Object.entries(state.disconnected).filter(([key]) => !key.endsWith(`/${siteId}`))) })),
}))

/** Whether the user disconnected from a site. */
export function useDisconnected(site: SiteRef): boolean {
  return useConnectionState((state) => state.disconnected[logKey(site.workspaceId, site.siteId)] === true)
}
