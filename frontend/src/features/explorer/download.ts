import type { RemoteEntry } from '@/lib/api'
import { walkTree } from '@/lib/tree-walk'

/** Most files a folder may hold to be downloaded one by one. */
export const maxEachFiles = 500

/** Milliseconds between the downloads of a folder fetched file by file, so the browser is not flooded. */
export const eachDelayMs = 300

/** Milliseconds a download is shown as being prepared at most, in case the server never reports it as a transfer. */
export const preparingTimeoutMs = 20_000

/** A folder holds more files than may be downloaded one by one. */
export class TooManyFiles extends Error {}

/**
 * Starts a browser download of a URL by clicking a temporary link.
 *
 * The link is marked as a download, so the page stays where it is even when the server answers with an error instead of
 * the file: the browser reports a failed download rather than leaving the app to show the error. The name still comes from
 * the server's `Content-Disposition`.
 */
export function startDownload(url: string): void {
  const link = document.createElement('a')
  link.href = url
  link.download = ''
  link.rel = 'noopener'
  document.body.append(link)
  link.click()
  link.remove()
}

/** Lists the files of a folder and of its subfolders, breadth first; links and special files are left out. */
export async function collectFiles(list: (path: string, signal?: AbortSignal) => Promise<RemoteEntry[]>, root: string, limit: number, signal?: AbortSignal): Promise<RemoteEntry[]> {
  const found: RemoteEntry[] = []
  await walkTree(list, root, (entry) => {
    if (entry.type === 'file') {
      found.push(entry)
      if (found.length > limit) throw new TooManyFiles()
    }
  }, signal)
  return found
}
