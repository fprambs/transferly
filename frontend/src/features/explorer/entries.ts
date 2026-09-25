import type { RemoteEntry } from '@/lib/api'

/** One step of a path, for breadcrumbs. */
export interface PathSegment {
  /** Name shown for the step; the root is `/`. */
  name: string
  /** Absolute path that leads to the step. */
  path: string
}

/** Units of size, from bytes up. */
const sizeUnits = ['B', 'KB', 'MB', 'GB', 'TB'] as const

/**
 * Sorts entries the way file managers do: directories first, then by name ignoring case and comparing digits as numbers.
 *
 * @returns A new array; the input is left as it is.
 */
export function sortEntries(entries: readonly RemoteEntry[]): RemoteEntry[] {
  const rank = (entry: RemoteEntry): number => (entry.type === 'directory' ? 0 : 1)
  return [...entries].sort(
    (a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }),
  )
}

/** Formats a size in bytes with binary steps and one decimal from kilobytes up, such as `1.5 MB`. */
export function formatSize(bytes: number): string {
  const exponent = bytes < 1024 ? 0 : Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), sizeUnits.length - 1)
  const value = bytes / 1024 ** exponent
  return exponent === 0 ? `${bytes} B` : `${value.toFixed(1)} ${sizeUnits[exponent]}`
}

/** Splits an absolute path into its steps, starting with the root. */
export function pathSegments(path: string): PathSegment[] {
  const names = path.split('/').filter((name) => name !== '')
  return [
    { name: '/', path: '/' },
    ...names.map((name, index) => ({ name, path: `/${names.slice(0, index + 1).join('/')}` })),
  ]
}

/** Returns the path of the directory that holds a path; the root is its own parent. */
export function parentPath(path: string): string {
  const segments = pathSegments(path)
  return segments[Math.max(segments.length - 2, 0)].path
}
