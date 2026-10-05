/** Query key of a folder listing. */
export function filesKey(workspaceId: string, siteId: string, path: string) {
  return ['workspaces', workspaceId, 'files', siteId, path] as const
}

/** Query key that every cached folder listing of a connection starts with, to reload or forget them together. */
export function filesPrefix(workspaceId: string, siteId: string) {
  return ['workspaces', workspaceId, 'files', siteId] as const
}

/** How long, in milliseconds, a listing counts as fresh: visiting a folder again within it shows the cached listing and asks the server nothing. */
export const listingFreshMs = 15_000

/** How long, in milliseconds, a listing that nobody is looking at stays cached to be shown at once on the next visit. */
export const listingKeepMs = 10 * 60_000

/** How long, in milliseconds, what the server of a connection can do on its own counts as known (ADR-0027). */
export const toolsFreshMs = 5 * 60_000
