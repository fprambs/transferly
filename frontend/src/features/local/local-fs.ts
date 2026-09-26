import type { RemoteEntry } from '@/lib/api'

/** Identifier the local folder pane uses where a site identifier goes, in the address and in the drag data. */
export const localSiteId = 'local'

/** A picker of directories, as browsers of the Chromium family offer it. */
type DirectoryPicker = (options?: { mode?: 'read' | 'readwrite' }) => Promise<FileSystemDirectoryHandle>

/** A directory handle that can be listed; the standard type of the library does not declare its entries. */
type ListableDirectory = FileSystemDirectoryHandle & { entries: () => AsyncIterable<[string, FileSystemHandle]> }

/** Reads the directory picker of the browser, or `undefined` when it has none. */
function picker(): DirectoryPicker | undefined {
  return (window as unknown as { showDirectoryPicker?: DirectoryPicker }).showDirectoryPicker
}

/** Tells whether the browser can open folders of the user's computer. */
export function localSupported(): boolean {
  return picker() !== undefined
}

/** Asks the user for a folder to work in; resolves to `null` when they close the dialog without choosing. */
export async function pickLocalDirectory(): Promise<FileSystemDirectoryHandle | null> {
  try {
    return await (picker() as DirectoryPicker)({ mode: 'readwrite' })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      return null
    }
    throw error
  }
}

/** Splits an absolute path into its names. */
function names(path: string): string[] {
  return path.split('/').filter((name) => name !== '')
}

/** Opens the folder at an absolute path below the root the user chose. */
export async function openLocalDirectory(root: FileSystemDirectoryHandle, path: string): Promise<FileSystemDirectoryHandle> {
  let current = root
  for (const name of names(path)) {
    current = await current.getDirectoryHandle(name)
  }
  return current
}

/** Lists a folder as entries of the same shape the remote panes show; their paths are absolute below the root. */
export async function listLocal(root: FileSystemDirectoryHandle, path: string): Promise<RemoteEntry[]> {
  const directory = await openLocalDirectory(root, path)
  const base = path === '/' ? '' : path
  const entries: RemoteEntry[] = []
  for await (const [name, handle] of (directory as ListableDirectory).entries()) {
    const entry: RemoteEntry = { name, path: `${base}/${name}`, type: 'directory', size: 0, mode: '', modifiedAt: new Date(0).toISOString(), modifiedPrecision: 'minute' }
    if (handle.kind === 'file') {
      const file = await (handle as FileSystemFileHandle).getFile()
      entry.type = 'file'
      entry.size = file.size
      entry.modifiedAt = new Date(file.lastModified).toISOString()
    }
    entries.push(entry)
  }
  return entries
}

/** Reads a file below the root the user chose. */
export async function readLocalFile(root: FileSystemDirectoryHandle, path: string): Promise<File> {
  const parts = names(path)
  const directory = await openLocalDirectory(root, parts.slice(0, -1).join('/'))
  return (await directory.getFileHandle(parts[parts.length - 1])).getFile()
}

/** Tells whether a folder already holds a file or folder with a name. */
async function exists(directory: FileSystemDirectoryHandle, name: string): Promise<boolean> {
  for (const lookup of [() => directory.getFileHandle(name), () => directory.getDirectoryHandle(name)]) {
    try {
      await lookup()
      return true
    } catch {
      continue
    }
  }
  return false
}

/** The name the folder already uses, so nothing of the user's is overwritten. */
export class LocalNameTaken extends Error {}

/** Writes a stream to a new file in a folder below the root; an existing name is refused and a failure leaves no partial file. */
export async function saveToLocal(root: FileSystemDirectoryHandle, path: string, name: string, data: ReadableStream<Uint8Array>): Promise<void> {
  const directory = await openLocalDirectory(root, path)
  if (await exists(directory, name)) {
    throw new LocalNameTaken(name)
  }
  const writable = await (await directory.getFileHandle(name, { create: true })).createWritable()
  const reader = data.getReader()
  const writer = writable.getWriter()
  try {
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) {
        break
      }
      await writer.write(chunk.value)
    }
    await writer.close()
  } catch (error) {
    void Promise.allSettled([reader.cancel()])
    await Promise.allSettled([writer.abort()])
    await Promise.allSettled([directory.removeEntry(name)])
    throw error
  }
}
