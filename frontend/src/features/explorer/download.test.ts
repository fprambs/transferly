import { describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { collectFiles, startDownload, TooManyFiles } from './download'

/**
 * Covers {@link startDownload}.
 *
 * Scenarios:
 * - a temporary link to the URL, marked as a download so an error never replaces the app, is clicked and removed again
 */
describe('startDownload', () => {
  it('clicks a temporary link and removes it', () => {
    const clicks: string[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicks.push(this.href)
      expect(this.hasAttribute('download')).toBe(true)
      expect(document.body.contains(this)).toBe(true)
    })

    startDownload('http://localhost/api/v1/files/content?path=%2Fa.txt')

    expect(clicks).toEqual(['http://localhost/api/v1/files/content?path=%2Fa.txt'])
    expect(document.querySelector('a[rel="noopener"]')).toBeNull()
  })
})

/** Builds an entry. */
function entry(path: string, type: RemoteEntry['type']): RemoteEntry {
  return { name: path.slice(path.lastIndexOf('/') + 1), path, type, size: 1, mode: '', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'minute' }
}

/**
 * Covers {@link collectFiles}.
 *
 * Scenarios:
 * - the files of a tree are collected folder by folder, leaving out links and other special entries
 * - a tree with more files than the limit fails with TooManyFiles, and one with exactly the limit does not
 */
describe('collectFiles', () => {
  const tree: Record<string, RemoteEntry[]> = {
    '/d': [entry('/d/a.txt', 'file'), entry('/d/sub', 'directory'), entry('/d/link', 'symlink'), entry('/d/dev', 'other')],
    '/d/sub': [entry('/d/sub/b.txt', 'file')],
  }
  const list = (path: string) => Promise.resolve(tree[path])

  it('collects the files of the whole tree', async () => {
    expect((await collectFiles(list, '/d', 10)).map((file) => file.path)).toEqual(['/d/a.txt', '/d/sub/b.txt'])
  })

  it('refuses a tree with more files than the limit', async () => {
    await expect(collectFiles(list, '/d', 1)).rejects.toBeInstanceOf(TooManyFiles)
    await expect(collectFiles(list, '/d', 2)).resolves.toHaveLength(2)
  })
})
