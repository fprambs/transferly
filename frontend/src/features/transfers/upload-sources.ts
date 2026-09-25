/** What is needed to continue an upload that stopped: the file itself and where it was going. */
export interface UploadSource {
  /** The file chosen by the user; the browser only lets a page keep it while the page stays open. */
  file: File
  /** Workspace of the connection. */
  workspaceId: string
  /** Saved site or quick connection the file was going to. */
  siteId: string
}

/** Most sources kept; the oldest are dropped first so a long session does not hold on to many big files. */
export const maxSources = 50

/** Sources by transfer identifier, oldest first. */
const sources = new Map<string, UploadSource>()

/** Remembers the source of an upload under its transfer identifier. */
export function rememberUpload(transferId: string, source: UploadSource): void {
  sources.delete(transferId)
  sources.set(transferId, source)
  if (sources.size > maxSources) {
    sources.delete(sources.keys().next().value as string)
  }
}

/** Returns the source of an upload, if this page still has it. */
export function recallUpload(transferId: string): UploadSource | undefined {
  return sources.get(transferId)
}

/** Forgets the source of an upload, once it finished or cannot be resumed. */
export function forgetUpload(transferId: string): void {
  sources.delete(transferId)
}

/** Forgets every source. */
export function clearUploadSources(): void {
  sources.clear()
}
