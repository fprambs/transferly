import type { Site } from '@/lib/api'

/** The path that stands for the folder the server opens a session in, as pwd tells it, until the explorer asks the server for it. */
export const homePath = '~'

/** The folder a saved site opens in: its default remote folder, or the folder its server opens sessions in (4.14i); a quick connection,
 * which has none, opens that folder too. */
export function startPath(site: Site | undefined): string {
  return site?.remoteDir ?? homePath
}
