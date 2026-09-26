import { listLocal, openLocalDirectory } from './local-fs'

/** A file of a folder that is being uploaded. */
export interface TreeFile {
  /** Path of the file below the folder, without a leading slash, such as `sub/a.txt`. */
  path: string
  /** The file itself. */
  file: File
}

/** A folder of the user's computer, read in full and ready to be uploaded. */
export interface UploadTree {
  /** Name of the folder. */
  name: string
  /** Paths of the folders below it, without a leading slash, parents before their children. */
  dirs: string[]
  /** The files of the folder and of its subfolders. */
  files: TreeFile[]
}

/** Most entries read from one folder; beyond it the read fails, so a huge disk cannot freeze the page. */
export const maxTreeEntries = 20_000

/** The folder holds more entries than may be uploaded at once. */
export class TreeTooLarge extends Error {}

/** What a drop carried: loose files and whole folders. */
export interface Dropped {
  /** Files dropped on their own. */
  files: File[]
  /** Folders dropped, read in full. */
  folders: UploadTree[]
}

/** The part of a directory entry of the browser that the reader uses. */
interface FsEntry {
  /** Whether the entry is a file. */
  isFile: boolean
  /** Whether the entry is a folder. */
  isDirectory: boolean
  /** Name of the entry. */
  name: string
  /** Opens a file entry. */
  file?: (done: (file: File) => void, fail: (error: unknown) => void) => void
  /** Starts reading the entries of a folder. */
  createReader?: () => { readEntries: (done: (entries: FsEntry[]) => void, fail: (error: unknown) => void) => void }
}

/** Reads every entry of a directory; the browser hands them out in batches until an empty one. */
async function readAll(entry: FsEntry): Promise<FsEntry[]> {
  const reader = (entry.createReader as NonNullable<FsEntry['createReader']>)()
  const all: FsEntry[] = []
  for (;;) {
    const batch = await new Promise<FsEntry[]>((done, fail) => reader.readEntries(done, fail))
    if (batch.length === 0) {
      return all
    }
    all.push(...batch)
  }
}

/** Reads a directory entry and everything below it into a tree. */
async function readEntry(folder: FsEntry): Promise<UploadTree> {
  const tree: UploadTree = { name: folder.name, dirs: [], files: [] }
  let count = 0
  const walk = async (entry: FsEntry, prefix: string): Promise<void> => {
    const children = await readAll(entry)
    count += children.length
    if (count > maxTreeEntries) {
      throw new TreeTooLarge()
    }
    for (const child of children) {
      const path = prefix + child.name
      if (child.isDirectory) {
        tree.dirs.push(path)
        await walk(child, `${path}/`)
      } else {
        const file = await new Promise<File>((done, fail) => (child.file as NonNullable<FsEntry['file']>)(done, fail))
        tree.files.push({ path, file })
      }
    }
  }
  await walk(folder, '')
  return tree
}

/** Reads what a drop carried, walking the folders it holds. */
export async function readDropped(data: DataTransfer): Promise<Dropped> {
  const dropped: Dropped = { files: [], folders: [] }
  const entries = Array.from(data.items ?? []).map((item) => (item as unknown as { webkitGetAsEntry?: () => FsEntry | null }).webkitGetAsEntry?.() ?? null)
  const files = Array.from(data.files)
  if (entries.length === 0 || entries.every((entry) => entry === null)) {
    dropped.files = files
    return dropped
  }
  for (const [index, entry] of entries.entries()) {
    if (entry?.isDirectory) {
      dropped.folders.push(await readEntry(entry))
    } else {
      dropped.files.push(...files.slice(index, index + 1))
    }
  }
  return dropped
}

/** Builds the tree of a folder picked with a `webkitdirectory` input, whose files carry their path below the picked folder. */
export function treeFromInput(files: readonly File[]): UploadTree | null {
  const first = files.find((file) => Boolean(file.webkitRelativePath))
  if (first === undefined) {
    return null
  }
  const name = first.webkitRelativePath.split('/')[0]
  const dirs = new Set<string>()
  const tree: UploadTree = { name, dirs: [], files: [] }
  for (const file of files) {
    const parts = file.webkitRelativePath.split('/').slice(1)
    for (let depth = 1; depth < parts.length; depth++) {
      dirs.add(parts.slice(0, depth).join('/'))
    }
    tree.files.push({ path: parts.join('/'), file })
  }
  tree.dirs = [...dirs].sort()
  return tree
}

/** Reads a folder of the folder the user chose, and everything below it, into a tree. */
export async function treeFromHandle(root: FileSystemDirectoryHandle, path: string): Promise<UploadTree> {
  const tree: UploadTree = { name: path.slice(path.lastIndexOf('/') + 1), dirs: [], files: [] }
  let count = 0
  const walk = async (folder: string, prefix: string): Promise<void> => {
    const entries = await listLocal(root, folder)
    count += entries.length
    if (count > maxTreeEntries) {
      throw new TreeTooLarge()
    }
    for (const entry of entries) {
      const relative = prefix + entry.name
      if (entry.type === 'directory') {
        tree.dirs.push(relative)
        await walk(entry.path, `${relative}/`)
      } else {
        const directory = await openLocalDirectory(root, folder)
        tree.files.push({ path: relative, file: await (await directory.getFileHandle(entry.name)).getFile() })
      }
    }
  }
  await walk(path, '')
  return tree
}
