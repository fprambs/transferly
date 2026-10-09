import type { RemoteEntry } from '@/lib/api'

/** Most entries, nesting levels and milliseconds a recursive operation may consume. */
export const treeLimits = { entries: 20_000, depth: 64, durationMs: 30_000 }

/** Raised before a recursive operation exceeds its work budget or traverses an unsafe entry. */
export class TreeLimitError extends Error {}

/** Traverses regular files and directories with bounded work, validated child paths and cancellation. */
export async function walkTree(
  list: (path: string, signal?: AbortSignal) => Promise<RemoteEntry[]>,
  root: string,
  visit: (entry: RemoteEntry) => boolean | void,
  signal?: AbortSignal,
): Promise<void> {
  root = root === '/' ? root : root.replace(/\/+$/, '')
  const deadline = AbortSignal.timeout(treeLimits.durationMs)
  signal = signal === undefined ? deadline : AbortSignal.any([signal, deadline])
  const started = Date.now()
  const pending = [{ path: root, depth: 0 }]
  const visited = new Set<string>()
  let count = 0
  for (let index = 0; index < pending.length; index++) {
    signal.throwIfAborted()
    const folder = pending[index]
    if (folder.depth > treeLimits.depth || Date.now() - started > treeLimits.durationMs || visited.has(folder.path)) throw new TreeLimitError()
    visited.add(folder.path)
    for (const entry of await list(folder.path, signal)) {
      signal.throwIfAborted()
      if (++count > treeLimits.entries || Date.now() - started > treeLimits.durationMs) throw new TreeLimitError()
      if (entry.type !== 'file' && entry.type !== 'directory') continue
      const child = `${folder.path === '/' ? '' : folder.path}/${entry.name}`
      if (entry.name === '' || entry.name === '.' || entry.name === '..' || /[/\\\0]/.test(entry.name) || entry.path !== child) throw new TreeLimitError()
      if (visit(entry) === false) return
      if (entry.type === 'directory') pending.push({ path: entry.path, depth: folder.depth + 1 })
    }
  }
}
