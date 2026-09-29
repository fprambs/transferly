import type { Site } from '@/lib/api'

/** The folder a saved site opens in: its default remote folder, or the root (4.14i); a quick connection, which has none, opens the root. */
export function startPath(site: Site | undefined): string {
  return site?.remoteDir ?? '/'
}
