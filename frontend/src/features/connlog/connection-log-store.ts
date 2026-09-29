import { create } from 'zustand'

import type { ConnectionLogEntry } from '@/lib/transport'

/** Most lines kept per site; older ones are dropped first. */
export const maxLogLines = 500

/** One line of a log: the entry the server sent and a number that orders it among all lines. */
export interface LogLine extends ConnectionLogEntry {
  /** Increasing number of the line, unique across sites. */
  seq: number
}

/** The log lines of every site, kept in memory only (ADR-0011). */
interface ConnectionLogState {
  /** Lines by site, keyed by {@link logKey}. */
  lines: Record<string, LogLine[]>
  /** Number given to the next line. */
  next: number
  /**
   * Adds an entry to the log of its site, dropping the oldest line past {@link maxLogLines}. A connection that starts after the site was
   * disconnected starts a log of its own, so opening a site again does not carry on the previous session; one that starts after a failure
   * keeps the failure above it.
   */
  add: (entry: ConnectionLogEntry) => void
  /** Empties the log of a site. */
  clear: (workspaceId: string, siteId: string) => void
}

/** Key of the log of a site in a workspace. */
export function logKey(workspaceId: string, siteId: string): string {
  return `${workspaceId}/${siteId}`
}

/** Store with the connection log of each site. */
export const useConnectionLog = create<ConnectionLogState>()((set) => ({
  lines: {},
  next: 1,
  add: (entry) =>
    set((state) => {
      const key = logKey(entry.workspaceId, entry.siteId)
      const previous = state.lines[key] ?? []
      const fresh = entry.step === 'connecting' && previous.at(-1)?.step === 'disconnected'
      const lines = [...(fresh ? [] : previous), { ...entry, seq: state.next }].slice(-maxLogLines)
      return { lines: { ...state.lines, [key]: lines }, next: state.next + 1 }
    }),
  clear: (workspaceId, siteId) => set((state) => ({ lines: { ...state.lines, [logKey(workspaceId, siteId)]: [] } })),
}))
