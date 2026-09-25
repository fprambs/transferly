import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { formatSize, joinPath, parentPath, pathSegments, resolveSelection, sortEntries } from './entries'

/** Builds an entry with only what sorting looks at. */
function entry(name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `/${name}`, type, size: 0, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second' }
}

/**
 * Covers the helpers of the explorer: {@link sortEntries}, {@link formatSize}, {@link pathSegments} and {@link parentPath}.
 *
 * Scenarios:
 * - directories come first, names sort ignoring case and numbers sort by value, and the input is not changed
 * - sizes use bytes below a kilobyte and one decimal above, up to terabytes and beyond
 * - a path splits into steps that start at the root, and ignores repeated slashes
 * - the parent of a path is the step before it, and the root is its own parent
 * - names join onto folders, including the root
 * - the selection is the entry with the selected path, or the first one when there is none or it is gone
 */
describe('explorer entries', () => {
  it('lists directories first and sorts names naturally', () => {
    const input = [entry('b.txt'), entry('file10'), entry('file2'), entry('Zeta', 'directory'), entry('alpha', 'directory'), entry('A.txt')]

    const sorted = sortEntries(input)

    expect(sorted.map((item) => item.name)).toEqual(['alpha', 'Zeta', 'A.txt', 'b.txt', 'file2', 'file10'])
    expect(input[0].name).toBe('b.txt')
  })

  it.each([
    [0, '0 B'],
    [1023, '1023 B'],
    [1024, '1.0 KB'],
    [1536, '1.5 KB'],
    [5 * 1024 ** 2, '5.0 MB'],
    [3 * 1024 ** 3, '3.0 GB'],
    [2 * 1024 ** 4, '2.0 TB'],
    [2048 * 1024 ** 4, '2048.0 TB'],
  ])('formats %d bytes as %s', (bytes, expected) => {
    expect(formatSize(bytes)).toBe(expected)
  })

  it('splits a path into steps', () => {
    expect(pathSegments('/var//www/site')).toEqual([
      { name: '/', path: '/' },
      { name: 'var', path: '/var' },
      { name: 'www', path: '/var/www' },
      { name: 'site', path: '/var/www/site' },
    ])
    expect(pathSegments('/')).toEqual([{ name: '/', path: '/' }])
  })

  it('finds the parent of a path', () => {
    expect(parentPath('/var/www')).toBe('/var')
    expect(parentPath('/var')).toBe('/')
    expect(parentPath('/')).toBe('/')
  })

  it('joins a name onto a folder', () => {
    expect(joinPath('/', 'a')).toBe('/a')
    expect(joinPath('/var/www', 'a')).toBe('/var/www/a')
  })

  it('resolves the selected entry', () => {
    const entries = [entry('a'), entry('b')]

    expect(resolveSelection(entries, '/b')).toBe(1)
    expect(resolveSelection(entries, '/gone')).toBe(0)
    expect(resolveSelection(entries, null)).toBe(0)
  })
})
