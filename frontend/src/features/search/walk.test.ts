import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { maxMatches, maxScanned, searchTree, type SearchMatch } from './walk'

/** Builds an entry. */
function entry(path: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name: path.slice(path.lastIndexOf('/') + 1), path, type, size: 1, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second' }
}

/** A tree of folders, by path, for a fake `list`. */
type Tree = Record<string, RemoteEntry[]>

/** Runs a search against a fake tree and collects the matches in the order they were found. */
async function run(tree: Tree, root: string, matches: (name: string) => boolean, signal: AbortSignal = new AbortController().signal) {
  const found: SearchMatch[] = []
  const summary = await searchTree((path) => Promise.resolve(tree[path] ?? []), root, matches, (match) => found.push(match), signal)
  return { found, summary }
}

/**
 * Covers {@link searchTree}.
 *
 * Scenarios:
 * - files and folders that match are reported with their path relative to the searched folder, breadth first
 * - a folder is walked into whether or not its own name matches, so a match can live several levels down
 * - symlinks and other special entries are left out, and never walked into
 * - searching from the root strips only the leading slash, and a match whose path is not actually below the searched folder, as a
 *   misbehaving server could send, keeps its path as it is
 * - stopping mid-search through an already aborted signal reports what was found so far, as canceled
 * - a folder listing that fails because it was aborted while in flight is reported as canceled too, but any other failure is not swallowed
 * - aborting partway through the entries of an already fetched folder stops the rest of them too
 * - a tree with more entries than the scan limit stops early and says so, without finishing
 * - more matches than the limit stops the search early too
 * - progress is reported after every entry looked at, when asked for
 */
describe('searchTree', () => {
  it('reports a traversal timeout as an incomplete search', async () => {
    const summary = await searchTree(async () => { throw new DOMException('deadline', 'TimeoutError') }, '/', () => true, () => {}, new AbortController().signal)
    expect(summary).toEqual({ scanned: 0, truncated: true, canceled: false })
  })
  it('reports matching files and folders with their relative path, breadth first', async () => {
    const tree: Tree = {
      '/root': [entry('/root/report.csv'), entry('/root/notes.txt'), { ...entry('/root/reports'), type: 'directory' }],
      '/root/reports': [entry('/root/reports/report-2026.csv')],
    }
    const { found, summary } = await run(tree, '/root', (name) => name.toLowerCase().includes('report'))
    expect(found.map((match) => match.relativePath)).toEqual(['report.csv', 'reports', 'reports/report-2026.csv'])
    expect(summary).toEqual({ scanned: 4, truncated: false, canceled: false })
  })

  it('walks into a folder whose own name does not match', async () => {
    const tree: Tree = { '/root': [{ ...entry('/root/archive'), type: 'directory' }], '/root/archive': [entry('/root/archive/deep-report.txt')] }
    const { found } = await run(tree, '/root', (name) => name.includes('report'))
    expect(found.map((match) => match.relativePath)).toEqual(['archive/deep-report.txt'])
  })

  it('strips only the leading slash when searching from the root', async () => {
    const { found } = await run({ '/': [entry('/report.txt')] }, '/', (name) => name.includes('report'))
    expect(found.map((match) => match.relativePath)).toEqual(['report.txt'])
  })

  it('refuses a path that is not below the searched folder', async () => {
    const { found } = await run({ '/root': [entry('/elsewhere/report.txt')] }, '/root', (name) => name.includes('report'))
    expect(found).toEqual([])
  })

  it('leaves out symlinks and other special entries, and never walks into them', async () => {
    const tree: Tree = { '/root': [entry('/root/report', 'symlink'), entry('/root/report2', 'other')] }
    const { found, summary } = await run(tree, '/root', (name) => name.includes('report'))
    expect(found).toEqual([])
    expect(summary.scanned).toBe(0)
  })

  it('reports what it found so far when the signal is already aborted', async () => {
    const controller = new AbortController()
    controller.abort()
    const tree: Tree = { '/root': [entry('/root/report.csv')] }
    const { found, summary } = await run(tree, '/root', () => true, controller.signal)
    expect(found).toEqual([])
    expect(summary).toEqual({ scanned: 0, truncated: false, canceled: true })
  })

  it('treats a folder listing that failed because it was aborted in flight as canceled, but not any other failure', async () => {
    const controller = new AbortController()
    const abortedList = (): Promise<RemoteEntry[]> => {
      controller.abort()
      return Promise.reject(new DOMException('stopped', 'AbortError'))
    }
    const summary = await searchTree(abortedList, '/root', () => true, () => {}, controller.signal)
    expect(summary).toEqual({ scanned: 0, truncated: false, canceled: true })

    const broken = new Error('network down')
    await expect(searchTree(() => Promise.reject(broken), '/root', () => true, () => {}, new AbortController().signal)).rejects.toBe(broken)
  })

  it('stops mid-folder when the signal is aborted between two entries already fetched', async () => {
    const controller = new AbortController()
    const tree: Tree = { '/root': [entry('/root/a.txt'), entry('/root/b.txt'), entry('/root/c.txt')] }
    const found: SearchMatch[] = []
    const summary = await searchTree(
      (path) => Promise.resolve(tree[path] ?? []),
      '/root',
      () => true,
      (match) => {
        found.push(match)
        if (match.relativePath === 'a.txt') {
          controller.abort()
        }
      },
      controller.signal,
    )
    expect(found).toEqual([{ relativePath: 'a.txt', entry: entry('/root/a.txt') }])
    expect(summary).toEqual({ scanned: 1, truncated: false, canceled: true })
  })

  it('stops once it has scanned the limit, without finishing the tree', async () => {
    const many = Array.from({ length: maxScanned + 5 }, (_, index) => entry(`/root/f${index}.txt`))
    const { summary } = await run({ '/root': many }, '/root', () => false)
    expect(summary).toEqual({ scanned: maxScanned, truncated: true, canceled: false })
  })

  it('stops once it has reported the limit of matches', async () => {
    const many = Array.from({ length: maxMatches + 5 }, (_, index) => entry(`/root/f${index}.txt`))
    const { found, summary } = await run({ '/root': many }, '/root', () => true)
    expect(found).toHaveLength(maxMatches)
    expect(summary.truncated).toBe(true)
  })

  it('reports progress after every entry', async () => {
    const tree: Tree = { '/root': [entry('/root/a.txt'), entry('/root/b.txt')] }
    const progress: number[] = []
    await searchTree((path) => Promise.resolve(tree[path] ?? []), '/root', () => false, () => {}, new AbortController().signal, (scanned) => progress.push(scanned))
    expect(progress).toEqual([1, 2])
  })
})
