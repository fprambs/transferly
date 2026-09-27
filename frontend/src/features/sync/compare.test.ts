import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { compareFiles, timeToleranceMs } from './compare'
import type { CollectedFile } from './collect'

/** Builds a file entry with a relative path, for one side of a comparison. */
function file(relativePath: string, size: number, modifiedAt: string): CollectedFile {
  const entry: RemoteEntry = { path: `/root/${relativePath}`, name: relativePath.slice(relativePath.lastIndexOf('/') + 1), type: 'file', size, mode: '-rw-r--r--', modifiedAt, modifiedPrecision: 'second' }
  return { relativePath, entry }
}

/**
 * Covers {@link compareFiles}.
 *
 * Scenarios:
 * - a file that exists only on the source is a create, and one that exists only on the target is a delete
 * - files with different sizes are an update, whatever their times, and files agreeing on size and time are left out
 * - a size that agrees but a time outside the tolerance is an update by time; right at the tolerance it still counts as equal
 * - a manual override wins over the time comparison, in both directions, but never over a size difference
 * - several files at once each get their own row, sorted by path, and comparing two empty folders gives nothing
 */
describe('compareFiles', () => {
  it('tells apart a file that exists on only one side', () => {
    const rows = compareFiles([file('a.txt', 10, '2026-01-01T00:00:00Z')], [file('b.txt', 10, '2026-01-01T00:00:00Z')])
    expect(rows).toEqual([
      { relativePath: 'a.txt', action: 'create', reason: 'missing', source: file('a.txt', 10, '2026-01-01T00:00:00Z').entry, target: null },
      { relativePath: 'b.txt', action: 'delete', reason: 'missing', source: null, target: file('b.txt', 10, '2026-01-01T00:00:00Z').entry },
    ])
  })

  it('tells different sizes apart whatever their times, and leaves equal files out', () => {
    const rows = compareFiles([file('a.txt', 10, '2026-01-01T00:00:00Z')], [file('a.txt', 20, '2026-01-01T00:00:00Z')])
    expect(rows).toEqual([{ relativePath: 'a.txt', action: 'update', reason: 'size', source: file('a.txt', 10, '2026-01-01T00:00:00Z').entry, target: file('a.txt', 20, '2026-01-01T00:00:00Z').entry }])
    expect(compareFiles([file('a.txt', 10, '2026-01-01T00:00:00Z')], [file('a.txt', 10, '2026-01-01T00:00:00Z')])).toEqual([])
  })

  it('applies the time tolerance', () => {
    const base = '2026-01-01T00:00:00Z'
    const withinMs = new Date(base).getTime() + timeToleranceMs
    const beyondMs = withinMs + 1000
    expect(compareFiles([file('a.txt', 10, base)], [file('a.txt', 10, new Date(withinMs).toISOString())])).toEqual([])
    const beyond = compareFiles([file('a.txt', 10, base)], [file('a.txt', 10, new Date(beyondMs).toISOString())])
    expect(beyond).toHaveLength(1)
    expect(beyond[0].reason).toBe('time')
  })

  it('lets a manual override win over the time comparison but never over a size difference', () => {
    const source = file('a.txt', 10, '2026-01-01T00:00:00Z')
    const target = file('a.txt', 10, '2026-06-01T00:00:00Z')
    expect(compareFiles([source], [target], new Map([['a.txt', true]]))).toEqual([])
    expect(compareFiles([source], [target], new Map([['a.txt', false]]))[0].reason).toBe('checksum')

    const differentSize = file('a.txt', 20, '2026-01-01T00:00:00Z')
    const sized = compareFiles([source], [differentSize], new Map([['a.txt', true]]))
    expect(sized[0].reason).toBe('size')
  })

  it('gives each differing file its own row, sorted by path', () => {
    const rows = compareFiles(
      [file('z.txt', 1, '2026-01-01T00:00:00Z'), file('a.txt', 2, '2026-01-01T00:00:00Z'), file('m/n.txt', 3, '2026-01-01T00:00:00Z')],
      [],
    )
    expect(rows.map((row) => row.relativePath)).toEqual(['a.txt', 'm/n.txt', 'z.txt'])
  })

  it('finds nothing between two empty folders', () => {
    expect(compareFiles([], [])).toEqual([])
  })
})
