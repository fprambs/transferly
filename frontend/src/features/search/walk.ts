import type { RemoteEntry } from '@/lib/api'
import { walkTree, TreeLimitError } from '@/lib/tree-walk'

/** Most entries a search reads before it stops looking further, so a pathological tree cannot run forever. */
export const maxScanned = 20_000

/** Most matches a search reports before it stops, so a very common pattern does not flood the results. */
export const maxMatches = 1000

/** One file or folder whose name matched, with its path relative to the folder the search started from. */
export interface SearchMatch {
  /** Path of the entry below the searched folder, without a leading slash. */
  relativePath: string
  /** The entry itself, with its absolute path. */
  entry: RemoteEntry
}

/** How a search ended. */
export interface SearchSummary {
  /** How many files and folders were looked at. */
  scanned: number
  /** Whether it stopped early because it reached {@link maxScanned} or {@link maxMatches}, rather than running out of tree. */
  truncated: boolean
  /** Whether it stopped because the caller asked it to. */
  canceled: boolean
}

/** Turns an absolute path below a folder into its path relative to that folder. */
function relativeTo(root: string, path: string): string {
  const prefix = root.endsWith('/') ? root : `${root}/`
  return path.slice(prefix.length)
}

/**
 * Walks a folder and everything below it, breadth first, reporting every file and folder whose name matches as soon as it is found, and
 * how many entries were looked at so far through `onProgress`, when given; symlinks and special entries are left out, like every other
 * tree walk of the explorer. It stops as soon as `signal` is aborted, and on its own once it has looked at {@link maxScanned} entries or
 * reported {@link maxMatches} matches.
 */
export async function searchTree(
  list: (path: string, signal?: AbortSignal) => Promise<RemoteEntry[]>,
  root: string,
  matches: (name: string) => boolean,
  onMatch: (match: SearchMatch) => void,
  signal: AbortSignal,
  onProgress?: (scanned: number) => void,
): Promise<SearchSummary> {
  let scanned = 0
  let found = 0
  let truncated = false
  try {
    await walkTree(list, root, (entry) => {
      scanned++
      onProgress?.(scanned)
      if (matches(entry.name)) {
        onMatch({ relativePath: relativeTo(root, entry.path), entry })
        found++
      }
      truncated = scanned >= maxScanned || found >= maxMatches
      return !truncated
    }, signal)
  } catch (error) {
    if (signal.aborted) return { scanned, truncated: false, canceled: true }
    if (error instanceof TreeLimitError || (error instanceof DOMException && error.name === 'TimeoutError')) truncated = true
    else throw error
  }
  return { scanned, truncated, canceled: signal.aborted }
}
