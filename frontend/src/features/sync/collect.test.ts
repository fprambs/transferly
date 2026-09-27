import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { collectRelative } from './collect'

/** Builds a file entry at an absolute path. */
function entry(path: string): RemoteEntry {
  return { name: path.slice(path.lastIndexOf('/') + 1), path, type: 'file', size: 1, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second' }
}

/**
 * Covers {@link collectRelative}.
 *
 * Scenarios:
 * - files below a folder are returned with their path relative to it, one level and several levels deep
 * - a folder compared at the root strips only the leading slash
 * - a file whose path is not actually below the compared folder, as a misbehaving server could send, keeps its path as it is
 */
describe('collectRelative', () => {
  it('makes paths relative to the compared folder', async () => {
    const tree: Record<string, RemoteEntry[]> = {
      '/site/backup': [entry('/site/backup/a.txt'), { ...entry('/site/backup/sub'), type: 'directory' }],
      '/site/backup/sub': [entry('/site/backup/sub/b.txt')],
    }
    const files = await collectRelative((path) => Promise.resolve(tree[path]), '/site/backup', 10)
    expect(files.map((file) => file.relativePath)).toEqual(['a.txt', 'sub/b.txt'])
  })

  it('strips only the leading slash when comparing at the root', async () => {
    const files = await collectRelative(() => Promise.resolve([entry('/a.txt')]), '/', 10)
    expect(files).toEqual([{ relativePath: 'a.txt', entry: entry('/a.txt') }])
  })

  it('keeps the path as it is when it is not below the compared folder', async () => {
    const files = await collectRelative(() => Promise.resolve([entry('/elsewhere/a.txt')]), '/site/backup', 10)
    expect(files).toEqual([{ relativePath: '/elsewhere/a.txt', entry: entry('/elsewhere/a.txt') }])
  })
})
