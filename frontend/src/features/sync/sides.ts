/** A site or quick connection, on one side of a comparison. */
export interface RemoteSyncSide {
  /** Discriminates the side. */
  kind: 'remote'
  /** Workspace the site belongs to. */
  workspaceId: string
  /** Saved site or quick connection. */
  siteId: string
  /** Absolute path of the folder being compared. */
  path: string
}

/** A folder of the user's computer, on one side of a comparison. */
export interface LocalSyncSide {
  /** Discriminates the side. */
  kind: 'local'
  /** The folder the user chose for the local pane. */
  root: FileSystemDirectoryHandle
  /** Absolute path, below that folder, of the folder being compared. */
  path: string
}

/** One side of a folder comparison: a remote folder or a local one. */
export type SyncSide = RemoteSyncSide | LocalSyncSide

/** Turns a path relative to a side's folder into the absolute path that side's own calls expect. */
export function sideAbsolutePath(side: SyncSide, relativePath: string): string {
  return side.path === '/' ? `/${relativePath}` : `${side.path}/${relativePath}`
}
