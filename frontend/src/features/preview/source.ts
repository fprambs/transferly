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
}

/** Cuts a range to a file of `size` bytes, and tells whether anything is left to read. */
function clamp(size: number, start: number, end: number): [number, number] {
  const from = Math.max(0, Math.min(start, size))
  return [from, Math.max(from, Math.min(end, size))]
}

/** A file of a remote site, read with ranged requests to its content. */
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
  }
}

/** A file of the local panel, read by slices of the file the browser gave access to. */
export function localSource(file: File, path: string): PreviewSource {
  return {
    key: `local:${path}`,
    name: file.name,
    size: file.size,
    modifiedAt: new Date(file.lastModified).toISOString(),
    read: async (start, end) => {
      const [from, to] = clamp(file.size, start, end)
      return new Uint8Array(await file.slice(from, to).arrayBuffer())
    },
  }
}
