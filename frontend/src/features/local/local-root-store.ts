import { create } from 'zustand'

/** The folder of the user's computer that the local pane works in. */
export interface LocalRootState {
  /** The folder the user chose, or `null` until they choose one. It lives in memory: a reload asks again. */
  root: FileSystemDirectoryHandle | null
  /** Grows every time a folder is chosen, so what was read from the previous one is not reused. */
  version: number
  /** Remembers the folder the user chose. */
  setRoot: (root: FileSystemDirectoryHandle | null) => void
}

/** Store of the folder chosen for the local pane. */
export const useLocalRoot = create<LocalRootState>()((set) => ({ root: null, version: 0, setRoot: (root) => set((state) => ({ root, version: state.version + 1 })) }))
