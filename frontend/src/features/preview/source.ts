import type { Api, RemoteEntry } from '@/lib/api'

/**
 * A file a viewer reads (ADR-0018): its name and size, and reads of a range of its bytes, so a viewer never needs the whole file when a
 * part is enough. Remote files, local files and, later, the entries of an archive are all sources.
 */
export interface PreviewSource {
  /** Stable identity of the file, to tell one source from another. */
  key: string
  /** Name of the file, with its extension. */
  name: string
  /** Size in bytes. */
  size: number
  /** When the file was last changed, as an ISO instant. */
  modifiedAt: string
  /** Reads the bytes from `start` included to `end` excluded, cut to the size of the file. */
  read(start: number, end: number, signal?: AbortSignal): Promise<Uint8Array>
  /**
   * An address the media player of the browser streams the file from, with ranged requests of its own, and how to let it go once the
   * player closes; left out, audio and video cannot be played.
   */
  mediaUrl?: () => Promise<MediaAddress>
}

/** An address a media player streams a file from. */
export interface MediaAddress {
  /** The address. */
  url: string
  /** Lets the address go when the player closes. */
  release: () => void
}

/** Cuts a range to a file of `size` bytes, and tells whether anything is left to read. */
function clamp(size: number, start: number, end: number): [number, number] {
  const from = Math.max(0, Math.min(start, size))
  return [from, Math.max(from, Math.min(end, size))]
}

/** A file of a remote site, read with ranged requests to its content, which the media player also streams from. */
export function remoteSource(api: Api, workspaceId: string, siteId: string, entry: RemoteEntry): PreviewSource {
  return {
    key: `remote:${workspaceId}:${siteId}:${entry.path}`,
    name: entry.name,
    size: entry.size,
    modifiedAt: entry.modifiedAt,
    read: async (start, end, signal) => {
      const [from, to] = clamp(entry.size, start, end)
      return from === to ? new Uint8Array() : api.files.read(workspaceId, siteId, entry.path, from, to, signal)
    },
    mediaUrl: async () => ({ url: api.files.downloadUrl(workspaceId, siteId, entry.path), release: () => undefined }),
  }
}

/** A file of the local panel, read by slices of the file the browser gives access to, and played from a blob URL; the file is opened
 * once, on first use. */
export function localSource(entry: RemoteEntry, open: () => Promise<File>): PreviewSource {
  let file: Promise<File> | undefined
  return {
    key: `local:${entry.path}`,
    name: entry.name,
    size: entry.size,
    modifiedAt: entry.modifiedAt,
    read: async (start, end) => {
      file ??= open()
      const opened = await file
      const [from, to] = clamp(opened.size, start, end)
      return new Uint8Array(await opened.slice(from, to).arrayBuffer())
    },
    mediaUrl: async () => {
      file ??= open()
      const url = URL.createObjectURL(await file)
      return { url, release: () => URL.revokeObjectURL(url) }
    },
  }
}
