import { create } from 'zustand'

/** A short notice that something was done. */
export interface Notice {
  /** Identifier of the notice, to take it away. */
  id: number
  /** What was done, in a sentence. */
  text: string
}

/** The notices on show; nothing here is persisted. */
export interface NoticesState {
  /** Notices on show, oldest first. */
  notices: Notice[]
  /** Identifier the next notice takes. */
  next: number
  /** Shows a notice. */
  notify: (text: string) => void
  /** Takes a notice away. */
  dismiss: (id: number) => void
}

/** How many notices show at once; older ones leave to make room. */
export const maxNotices = 3

/** Store of the notices, so any part of the shell can say that something was done. */
export const useNotices = create<NoticesState>()((set) => ({
  notices: [],
  next: 1,
  notify: (text) => set((state) => ({ notices: [...state.notices, { id: state.next, text }].slice(-maxNotices), next: state.next + 1 })),
  dismiss: (id) => set((state) => ({ notices: state.notices.filter((notice) => notice.id !== id) })),
}))
