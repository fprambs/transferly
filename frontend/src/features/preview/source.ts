import type { Api, ArchiveEntry, ArchiveListing, RemoteEntry } from '@/lib/api'

/**
 * A file a viewer reads (ADR-0018): its name and size, and reads of a range of its bytes, so a viewer never needs the whole file when a
 * part is enough. Remote files, local files and the entries of an archive are all sources.
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
  /** The entries of the file when it is an archive, and how to open them; left out, an archive shows as bytes. */
  archive?: ArchiveAccess
}

/** The entries of an archive and how to open each one (D2.5): read by the server for a remote archive. */
export interface ArchiveAccess {
  /** Lists the entries of the archive. */
  list: (signal?: AbortSignal) => Promise<ArchiveListing>
  /** Opens a file entry as a source of its own, which is an archive again when the entry is one. */
  open: (entry: ArchiveEntry) => Promise<PreviewSource>
  /** The address an entry downloads from; left out, entries cannot be downloaded one by one. */
  downloadUrl?: (entry: ArchiveEntry) => string
}

/** The most bytes of an archive entry the viewer reads, since an entry is held in memory while it is shown. */
export const maxEntryBytes = 64 * 1024 * 1024

/** An archive entry larger than {@link maxEntryBytes}. */
export class EntryTooLarge extends Error {
  /** Name of the error. */
  override name = 'EntryTooLarge'
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

/** A file of a remote site, read with ranged requests to its content, which the media player also streams from, and whose entries the
 * server lists when it is an archive. */
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
    archive: remoteArchive(api, workspaceId, siteId, entry.path, []),
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

/** Reads a stream whole, and fails with {@link EntryTooLarge} as soon as it goes past `limit` bytes. */
export async function readCapped(stream: ReadableStream<Uint8Array>, limit = maxEntryBytes): Promise<Uint8Array> {
  const reader = stream.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  for (let next = await reader.read(); !next.done; next = await reader.read()) {
    total += next.value.length
    if (total > limit) {
      await reader.cancel()
      throw new EntryTooLarge()
    }
    chunks.push(next.value)
  }
  const whole = new Uint8Array(total)
  let at = 0
  for (const chunk of chunks) {
    whole.set(chunk, at)
    at += chunk.length
  }
  return whole
}

/** A file held in memory, played from a blob URL, with its entries when it is an archive: an entry of an archive or an attachment of a
 * message. */
export function memorySource(key: string, name: string, modifiedAt: string, data: Uint8Array, archive?: ArchiveAccess): PreviewSource {
  return {
    key,
    name,
    size: data.length,
    modifiedAt,
    archive,
    read: async (start, end) => {
      const [from, to] = clamp(data.length, start, end)
      return data.slice(from, to)
    },
    mediaUrl: async () => {
      const url = URL.createObjectURL(new Blob([data as Uint8Array<ArrayBuffer>]))
      return { url, release: () => URL.revokeObjectURL(url) }
    },
  }
}

/** The base name of an entry of an archive, whose name is a path with slashes. */
export function entryName(entry: ArchiveEntry): string {
  return entry.name.split('/').filter((part) => part !== '').at(-1) ?? entry.name
}

/**
 * Opens an entry of an archive as a source held in memory, reading it whole at once, so a failure shows while opening it. An entry that
 * declares more than {@link maxEntryBytes} is refused before reading, and `load` fails past them for one that does not tell its size.
 */
export async function entrySource(key: string, entry: ArchiveEntry, load: () => Promise<Uint8Array>, archive?: ArchiveAccess): Promise<PreviewSource> {
  if (entry.size > maxEntryBytes) {
    throw new EntryTooLarge()
  }
  return memorySource(key, entryName(entry), entry.modified ?? '', await load(), archive)
}

/** The entries of a remote archive, or of the archive at the end of `inner` nested in it, read by the server. */
export function remoteArchive(api: Api, workspaceId: string, siteId: string, path: string, inner: readonly string[]): ArchiveAccess {
  const key = [`remote:${workspaceId}:${siteId}:${path}`, ...inner].join('!')
  return {
    list: (signal) => api.files.archiveEntries(workspaceId, siteId, path, inner, signal),
    open: (entry) =>
      entrySource(
        `${key}!${entry.name}`,
        entry,
        async () => readCapped(await api.files.archiveEntry(workspaceId, siteId, path, inner, entry.name)),
        remoteArchive(api, workspaceId, siteId, path, [...inner, entry.name]),
      ),
    downloadUrl: (entry) => api.files.archiveEntryUrl(workspaceId, siteId, path, inner, entry.name),
  }
}
