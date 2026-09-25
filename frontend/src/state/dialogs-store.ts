import { create } from 'zustand'

/** Which global dialogs are open; nothing here is persisted. */
export interface DialogsState {
  /** Whether the quick connect dialog is open. */
  quickConnectOpen: boolean
  /** Opens or closes the quick connect dialog. */
  setQuickConnectOpen: (open: boolean) => void
}

/** Store of the global dialogs, so any part of the shell can open them. */
export const useDialogsStore = create<DialogsState>()((set) => ({
  quickConnectOpen: false,
  setQuickConnectOpen: (quickConnectOpen) => set({ quickConnectOpen }),
}))
