import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { defaultSort, firstDirection, nextSort, type EntrySort } from '@/lib/entry-sort'

import { formatSize, joinPath, normalizePath, parentPath, pathSegments, resolveSelection, sortEntries } from './entries'

/** Builds an entry with only what sorting looks at. */
function entry(name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `/${name}`, type, size: 0, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second' }
}

/**
 * Covers the helpers of the explorer: {@link sortEntries}, {@link formatSize}, {@link pathSegments}, {@link parentPath} and {@link normalizePath}.
 *
 * Scenarios:
 * - directories come first, names sort ignoring case and numbers sort by value, and the input is not changed
 * - sizes use bytes below a kilobyte and one decimal above, up to terabytes and beyond
 * - a path splits into steps that start at the root, and ignores repeated slashes
 * - a typed path becomes absolute and plain: spaces around it, repeated and trailing slashes and `.` go, `..` climbs but not above the root
 * - the parent of a path is the step before it, and the root is its own parent
 * - names join onto folders, including the root
 * - entries sort by name, size, type and time in both directions, folders first and the newest first by default, ties by name; each
 *   column starts in its own direction and a second click flips it
 * - the selection is the entry with the selected path, or the first one when there is none or it is gone
 */
describe('explorer entries', () => {
  it('lists directories first and sorts names naturally', () => {
    const input = [entry('b.txt'), entry('file10'), entry('file2'), entry('Zeta', 'directory'), entry('alpha', 'directory'), entry('A.txt')]

    const sorted = sortEntries(input, { key: 'name', direction: 'asc' })

    expect(sorted.map((item) => item.name)).toEqual(['alpha', 'Zeta', 'A.txt', 'b.txt', 'file2', 'file10'])
    expect(input[0].name).toBe('b.txt')
  })

  it('sorts by every column in both directions, folders first, the newest first by default and ties by name', () => {
    const at = (name: string, type: RemoteEntry['type'], size: number, modifiedAt: string): RemoteEntry => ({ ...entry(name, type), size, modifiedAt })
    const input = [
      at('old.zip', 'file', 300, '2026-01-01T00:00:00Z'),
      at('new.txt', 'file', 100, '2026-03-01T00:00:00Z'),
      at('mid.csv', 'file', 200, '2026-02-01T00:00:00Z'),
      at('noext', 'file', 200, '2026-02-01T00:00:00Z'),
      at('docs', 'directory', 0, '2025-01-01T00:00:00Z'),
      at('build', 'directory', 0, '2026-04-01T00:00:00Z'),
    ]
    const names = (sort?: EntrySort) => sortEntries(input, sort).map((item) => item.name)
    expect(names()).toEqual(['build', 'docs', 'new.txt', 'mid.csv', 'noext', 'old.zip'])
    expect(names({ key: 'modified', direction: 'asc' })).toEqual(['docs', 'build', 'old.zip', 'mid.csv', 'noext', 'new.txt'])
    expect(names({ key: 'size', direction: 'desc' })).toEqual(['build', 'docs', 'old.zip', 'mid.csv', 'noext', 'new.txt'])
    expect(names({ key: 'size', direction: 'asc' })).toEqual(['build', 'docs', 'new.txt', 'mid.csv', 'noext', 'old.zip'])
    expect(names({ key: 'type', direction: 'asc' })).toEqual(['build', 'docs', 'noext', 'mid.csv', 'new.txt', 'old.zip'])
    expect(names({ key: 'name', direction: 'desc' })).toEqual(['docs', 'build', 'old.zip', 'noext', 'new.txt', 'mid.csv'])
  })

  it('starts each column in its own direction and flips the column in use', () => {
    expect(firstDirection('name')).toBe('asc')
    expect(firstDirection('type')).toBe('asc')
    expect(firstDirection('size')).toBe('desc')
    expect(firstDirection('modified')).toBe('desc')
    expect(nextSort(defaultSort, 'modified')).toEqual({ key: 'modified', direction: 'asc' })
    expect(nextSort({ key: 'modified', direction: 'asc' }, 'modified')).toEqual({ key: 'modified', direction: 'desc' })
    expect(nextSort(defaultSort, 'name')).toEqual({ key: 'name', direction: 'asc' })
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

  it('makes a typed path absolute and plain', () => {
    expect(normalizePath('  var//www/ ')).toBe('/var/www')
    expect(normalizePath('/var/./log/../www/')).toBe('/var/www')
    expect(normalizePath('../../etc')).toBe('/etc')
    expect(normalizePath('')).toBe('/')
  })
})
