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

/** Raised when a name would leave the folder it is written in, as a remote server could send to trick a download. */
export class UnsafeLocalName extends Error {}

/** Refuses the names that are empty, point at the folder itself or its parent, or hold a separator or a NUL character. */
function checkName(name: string): string {
  if (name === '' || name === '.' || name === '..' || /[/\\\0]/.test(name)) {
    throw new UnsafeLocalName(name)
  }
  return name
}

/** Splits an absolute path into its names, refusing the ones that would climb out of the root. */
function names(path: string): string[] {
  return path.split('/').filter((name) => name !== '').map(checkName)
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
export async function localNameExists(root: FileSystemDirectoryHandle, path: string, name: string): Promise<boolean> {
  return exists(await openLocalDirectory(root, path), name)
}

/** Returns the first name of the form `name (n).ext` that the folder does not use. */
export async function freeLocalName(root: FileSystemDirectoryHandle, path: string, name: string): Promise<string> {
  const directory = await openLocalDirectory(root, path)
  const dot = name.lastIndexOf('.') > 0 ? name.lastIndexOf('.') : name.length
  for (let n = 1; ; n++) {
    const candidate = `${name.slice(0, dot)} (${n})${name.slice(dot)}`
    if (!(await exists(directory, candidate))) {
      return candidate
    }
  }
}

/** Creates a folder below the root, or uses the one that exists; a file with that name is refused. */
export async function makeLocalDirectory(root: FileSystemDirectoryHandle, path: string, name: string): Promise<void> {
  const directory = await openLocalDirectory(root, path)
  checkName(name)
  try {
    await directory.getDirectoryHandle(name, { create: true })
  } catch {
    throw new LocalNameTaken(name)
  }
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

/** Writes a stream to a file in a folder below the root; an existing name is refused unless overwriting was asked for, and a failure leaves no partial file. */
export async function saveToLocal(root: FileSystemDirectoryHandle, path: string, name: string, data: ReadableStream<Uint8Array>, overwrite = false): Promise<void> {
  const directory = await openLocalDirectory(root, path)
  checkName(name)
  if (!overwrite && (await exists(directory, name))) {
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
