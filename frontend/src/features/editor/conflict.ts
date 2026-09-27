/** The size and modification time of a file the editor last knew about, whether from opening or saving it. */
export interface FileStamp {
  /** Size in bytes. */
  size: number
  /** Modification time, as the server reports it. */
  modifiedAt: string
}

/** Reports whether a fresh stamp differs from the one the editor is working from, meaning something else changed
 * the file since. */
export function stampChanged(baseline: FileStamp, fresh: FileStamp): boolean {
  return baseline.size !== fresh.size || baseline.modifiedAt !== fresh.modifiedAt
}
