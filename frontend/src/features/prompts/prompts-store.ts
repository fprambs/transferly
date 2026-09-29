import { create } from 'zustand'

import type { ConnectionPrompt } from '@/lib/transport'

/** The questions servers are asking right now, in the order they arrived (ADR-0016). Kept in memory only. */
interface PromptsState {
  /** Prompts waiting for an answer, oldest first. */
  waiting: ConnectionPrompt[]
  /** Adds a prompt that arrived; one already waiting is not added twice. */
  add: (prompt: ConnectionPrompt) => void
  /** Drops a prompt that no longer waits: answered, declined, or given up by the server, from this tab or another. */
  close: (id: string) => void
}

/** Store with the prompts waiting for the person connecting. */
export const usePrompts = create<PromptsState>()((set) => ({
  waiting: [],
  add: (prompt) => set((state) => (state.waiting.some((known) => known.id === prompt.id) ? state : { waiting: [...state.waiting, prompt] })),
  close: (id) => set((state) => ({ waiting: state.waiting.filter((prompt) => prompt.id !== id) })),
}))
