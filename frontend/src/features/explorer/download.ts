import type { RemoteEntry } from '@/lib/api'

/** Most files a folder may hold to be downloaded one by one. */
export const maxEachFiles = 500

/** Milliseconds between the downloads of a folder fetched file by file, so the browser is not flooded. */
export const eachDelayMs = 300

/** A folder holds more files than may be downloaded one by one. */
export class TooManyFiles extends Error {}

/**
 * Starts a browser download of a URL by clicking a temporary link.
 *
 * The server answers with `Content-Disposition: attachment`, so the page stays where it is.
 */
export function startDownload(url: string): void {
  const link = document.createElement('a')
  link.href = url
  link.rel = 'noopener'
  document.body.append(link)
  link.click()
  link.remove()
}

/** Lists the files of a folder and of its subfolders, breadth first; links and special files are left out. */
export async function collectFiles(list: (path: string) => Promise<RemoteEntry[]>, root: string, limit: number): Promise<RemoteEntry[]> {
  const found: RemoteEntry[] = []
  const pending = [root]
  for (let folder = pending.shift(); folder !== undefined; folder = pending.shift()) {
    for (const entry of await list(folder)) {
      if (entry.type === 'directory') {
        pending.push(entry.path)
      } else if (entry.type === 'file') {
        found.push(entry)
        if (found.length > limit) {
          throw new TooManyFiles()
        }
      }
    }
  }
  return found
}
