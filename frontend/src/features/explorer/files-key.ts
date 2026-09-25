/** Query key of a folder listing. */
export function filesKey(workspaceId: string, siteId: string, path: string) {
  return ['workspaces', workspaceId, 'files', siteId, path] as const
}
