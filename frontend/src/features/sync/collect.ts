import type { RemoteEntry } from '@/lib/api'

import { collectFiles, maxEachFiles, TooManyFiles } from '../explorer/download'

export { maxEachFiles, TooManyFiles }

/** A file found below the folder being compared, with its path relative to that folder. */
export interface CollectedFile {
  /** Path of the file below the compared folder, without a leading slash. */
  relativePath: string
  /** The entry itself, with its absolute path. */
  entry: RemoteEntry
}

/** Turns an absolute path below a folder into its path relative to that folder. */
function relativeTo(root: string, path: string): string {
  const prefix = root.endsWith('/') ? root : `${root}/`
  return path.startsWith(prefix) ? path.slice(prefix.length) : path
}

/**
 * Lists every file below a folder, breadth first, with its path relative to that folder. Symlinks and special files are left
 * out, like every tree walk of the explorer; a folder past the limit throws {@link TooManyFiles}.
 */
export async function collectRelative(list: (path: string) => Promise<RemoteEntry[]>, root: string, limit: number): Promise<CollectedFile[]> {
  const files = await collectFiles(list, root, limit)
  return files.map((entry) => ({ relativePath: relativeTo(root, entry.path), entry }))
}
