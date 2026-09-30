import { describe, expect, it } from 'vitest'

import type { ArchiveEntry } from '@/lib/api'

import { buildTree, initiallyOpen, isUnsafeName, looksLikeBomb, totals, visibleRows } from './archive-tree'

/** An entry of the given name, a file unless the name ends with a slash. */
function entry(name: string, size = 10): ArchiveEntry {
  return { name, type: name.endsWith('/') ? 'directory' : 'file', size, packed: size }
}

/** The rows of a tree as indented names. */
function names(rows: ReturnType<typeof visibleRows>): string[] {
  return rows.map((row) => `${'  '.repeat(row.depth)}${row.node.name}`)
}

/**
 * Covers the tree of the archive viewer (D2.5).
 *
 * Scenarios:
 * - folders implied by the paths of files get nodes, folders come before files and names sort with numbers in order; a listed folder
 *   keeps its entry, an entry listed twice keeps the last listing, and empty names are skipped
 * - names that are absolute, name a drive, use backslashes or climb to the parent are unsafe, and stay at the top under their whole name
 * - the chain of lone folders starts open, unsafe names aside, and only open folders show their children
 * - a filter shows the matching files and the folders on their way, every folder open
 * - totals count files and the sizes the archive tells, and a ratio over 200 past a megabyte looks like a bomb
 */
describe('archive tree', () => {
  it('builds folders from the paths of files, sorted', () => {
    const root = buildTree([entry('p/b/file10.txt'), entry('p/b/file9.txt'), entry('p/a.txt'), entry('p/b/'), entry('p/a.txt', 99), entry('/'), entry('p//c.txt')])
    expect(names(visibleRows(root, new Set(['p', 'p/b'])))).toEqual(['p', '  b', '    file9.txt', '    file10.txt', '  a.txt', '  c.txt'])
    const folder = root.children[0].children[0]
    expect(folder).toMatchObject({ path: 'p/b', folder: true, entry: { name: 'p/b/' } })
    expect(root.children[0].children[1].entry?.size).toBe(99)
    expect(root.children[0].entry).toBeUndefined()
  })

  it('keeps unsafe names at the top, whole', () => {
    expect(['/etc/passwd', 'C:\\x', 'a\\b', '../x', 'a/../../b'].every(isUnsafeName)).toBe(true)
    expect(['a/b', 'a..b/c', '<img src=x>.txt'].some(isUnsafeName)).toBe(false)
    const root = buildTree([entry('../../etc/passwd'), entry('safe/a.txt'), entry('/abs/')])
    expect(root.children.map((node) => [node.name, node.unsafe, node.folder])).toEqual([
      ['safe', false, true],
      ['../../etc/passwd', true, false],
      ['/abs/', true, false],
    ])
  })

  it('opens the chain of lone folders', () => {
    const root = buildTree([entry('a/b/c/d.txt'), entry('a/b/e.txt'), entry('a/b/f/g.txt')])
    const open = initiallyOpen(root)
    expect([...open]).toEqual(['a', 'a/b'])
    expect(names(visibleRows(root, open))).toEqual(['a', '  b', '    c', '    f', '    e.txt'])
    expect([...initiallyOpen(buildTree([entry('x.txt')]))]).toEqual([])
    expect([...initiallyOpen(buildTree([entry('a/x.txt'), entry('../y')]))]).toEqual(['a'])
  })

  it('filters by path, opening the folders on the way', () => {
    const root = buildTree([entry('a/b/Match.txt'), entry('a/other.txt'), entry('c/none.txt')])
    expect(names(visibleRows(root, new Set(), ' match '))).toEqual(['a', '  b', '    Match.txt'])
    expect(visibleRows(root, new Set(), 'zzz')).toEqual([])
  })

  it('counts files and bytes, and spots bombs', () => {
    expect(totals([entry('a/'), entry('a/x', 5), { ...entry('b'), size: -1 }])).toEqual({ files: 2, bytes: 5 })
    expect(looksLikeBomb({ ...entry('z'), size: 300 * 1024 * 1024, packed: 300 * 1024 })).toBe(true)
    expect(looksLikeBomb({ ...entry('z'), size: 1024 * 1024, packed: 1 })).toBe(false)
    expect(looksLikeBomb({ ...entry('z'), size: 300 * 1024 * 1024, packed: -1 })).toBe(false)
    expect(looksLikeBomb({ ...entry('z'), size: 300 * 1024 * 1024, packed: 100 * 1024 * 1024 })).toBe(false)
  })
})
