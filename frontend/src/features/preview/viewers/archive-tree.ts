import type { ArchiveEntry } from '@/lib/api'

/** A folder or a file of an archive, as the tree of the archive viewer shows it. */
export interface ArchiveNode {
  /** The last part of its path. */
  name: string
  /** Its path in the tree, the parts joined by slashes; the root has an empty path. */
  path: string
  /** The entry of the archive, missing for a folder the archive only implies through the paths of its files. */
  entry?: ArchiveEntry
  /** Whether it holds other nodes. */
  folder: boolean
  /** Its folders and then its files, each sorted by name. */
  children: ArchiveNode[]
  /** Whether the name stored in the archive could climb out of a folder or name a place of its own if it were extracted (D2.5). */
  unsafe: boolean
}

/** A node as a row of the tree, with how deep it is. */
export interface ArchiveRow {
  /** The node. */
  node: ArchiveNode
  /** How many folders it is inside, from 0. */
  depth: number
}

/**
 * Tells whether a name stored in an archive would be dangerous to extract as it is: an absolute path, a Windows drive or backslashes, or a
 * part that climbs to the parent folder. The viewer never extracts anything; it flags such names so the person looking knows.
 */
export function isUnsafeName(name: string): boolean {
  return name.startsWith('/') || /^[a-z]:/i.test(name) || name.includes('\\') || name.split('/').includes('..')
}

/** Sorts folders before files, then by name, with numbers in their order. */
function sortTree(node: ArchiveNode, collator: Intl.Collator): void {
  node.children.sort((a, b) => Number(b.folder) - Number(a.folder) || collator.compare(a.name, b.name))
  node.children.forEach((child) => sortTree(child, collator))
}

/**
 * Builds the tree of the entries of an archive. Folders an archive only implies through the paths of its files get a node of their own,
 * empty parts of a path are skipped, and an entry listed twice keeps its last listing. An entry with an unsafe name is not placed where
 * its name points: it stays at the top, under its whole name as the archive stores it, so it cannot hide in a folder named `..`.
 */
export function buildTree(entries: readonly ArchiveEntry[], locale?: string): ArchiveNode {
  const root: ArchiveNode = { name: '', path: '', folder: true, children: [], unsafe: false }
  const folders = new Map<string, ArchiveNode>([['', root]])
  const folderOf = (parts: readonly string[]): ArchiveNode => {
    const path = parts.join('/')
    const known = folders.get(path)
    if (known !== undefined) {
      return known
    }
    const node: ArchiveNode = { name: parts.at(-1) as string, path, folder: true, children: [], unsafe: false }
    folderOf(parts.slice(0, -1)).children.push(node)
    folders.set(path, node)
    return node
  }
  const files = new Map<string, ArchiveNode>()
  for (const entry of entries) {
    const parts = entry.name.split('/').filter((part) => part !== '')
    if (parts.length === 0) {
      continue
    }
    if (isUnsafeName(entry.name)) {
      root.children.push({ name: entry.name, path: `\u0000${entry.name}`, entry, folder: false, children: [], unsafe: true })
      continue
    }
    if (entry.type === 'directory') {
      folderOf(parts).entry = entry
      continue
    }
    const path = parts.join('/')
    const known = files.get(path)
    if (known !== undefined) {
      known.entry = entry
      continue
    }
    const node: ArchiveNode = { name: parts.at(-1) as string, path, entry, folder: false, children: [], unsafe: false }
    folderOf(parts.slice(0, -1)).children.push(node)
    files.set(path, node)
  }
  sortTree(root, new Intl.Collator(locale, { numeric: true, sensitivity: 'base' }))
  return root
}

/** The folders that start open: the chain of lone folders from the top, as a project packed in one folder; unsafe names at the top do
 * not count. */
export function initiallyOpen(root: ArchiveNode): Set<string> {
  const open = new Set<string>()
  for (let level = root.children.filter((node) => !node.unsafe); level.length === 1 && level[0].folder; level = level[0].children) {
    open.add(level[0].path)
  }
  return open
}

/**
 * The rows the tree shows: every node under an open folder, depth first. With a filter, only the files whose path holds it, ignoring case,
 * and the folders on their way are shown, every folder open.
 */
export function visibleRows(root: ArchiveNode, open: ReadonlySet<string>, filter = ''): ArchiveRow[] {
  const needle = filter.trim().toLowerCase()
  const rows: ArchiveRow[] = []
  const matches = (node: ArchiveNode): boolean =>
    node.folder ? node.children.some(matches) : node.path.toLowerCase().includes(needle)
  const walk = (node: ArchiveNode, depth: number): void => {
    for (const child of node.children) {
      if (needle !== '' && !matches(child)) {
        continue
      }
      rows.push({ node: child, depth })
      if (child.folder && (needle !== '' || open.has(child.path))) {
        walk(child, depth + 1)
      }
    }
  }
  walk(root, 0)
  return rows
}

/** How many files the tree holds, and how many bytes they take once extracted, counting the sizes the archive tells. */
export function totals(entries: readonly ArchiveEntry[]): { files: number; bytes: number } {
  const files = entries.filter((entry) => entry.type !== 'directory')
  return { files: files.length, bytes: files.reduce((sum, entry) => sum + Math.max(entry.size, 0), 0) }
}

/**
 * Tells whether an entry declares a compression ratio no real file has, over 200 to 1 past a megabyte, the same rule the server refuses
 * decompression bombs by.
 */
export function looksLikeBomb(entry: ArchiveEntry): boolean {
  return entry.packed > 0 && entry.size > 1024 * 1024 && entry.size / entry.packed > 200
}
