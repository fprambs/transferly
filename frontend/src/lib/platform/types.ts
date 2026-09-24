/** Capabilities of the environment the interface runs in. */
export interface Platform {
  /** Kind of shell hosting the interface. */
  kind: 'web' | 'desktop'
  /** Whether local disk folders can be browsed as a file pane. */
  hasLocalFs: boolean
  /** Whether whole folders can be chosen for upload. */
  canPickFolders: boolean
}

/** Capabilities of the browser build. */
export const webPlatform: Platform = {
  kind: 'web',
  hasLocalFs: false,
  canPickFolders: true,
}
