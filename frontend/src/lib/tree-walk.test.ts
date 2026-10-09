import { describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'

import { treeLimits, TreeLimitError, walkTree } from './tree-walk'

/** Builds one directory of a hostile or ordinary tree. */
function directory(path: string): RemoteEntry {
  return { name: path.split('/').pop() ?? '', path, type: 'directory', size: 0, mode: '', modifiedAt: '', modifiedPrecision: 'second' }
}

/** Tests entry/depth budgets, duplicate paths, cancellation and traversal control. */
describe('bounded tree traversal', () => {
  it('bounds empty-directory work, rejects duplicate paths and unsafe children', async () => {
    const saved = treeLimits.entries
    treeLimits.entries = 2
    try {
      await expect(walkTree(async (path) => [directory(`${path === '/' ? '' : path}/d`)], '/', () => {})).rejects.toBeInstanceOf(TreeLimitError)
      await expect(walkTree(async () => [directory('/d'), directory('/d')], '/', () => {})).rejects.toBeInstanceOf(TreeLimitError)
      await expect(walkTree(async () => [directory('/other/file')], '/', () => {})).rejects.toBeInstanceOf(TreeLimitError)
    } finally { treeLimits.entries = saved }
  })

  it('stops at depth, on cancellation and when the visitor is done', async () => {
    const saved = treeLimits.depth
    treeLimits.depth = 0
    try { await expect(walkTree(async () => [directory('/d')], '/', () => {})).rejects.toBeInstanceOf(TreeLimitError) }
    finally { treeLimits.depth = saved }
    const controller = new AbortController()
    controller.abort()
    await expect(walkTree(async () => [], '/', () => {}, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    let reads = 0
    await walkTree(async () => { reads++; return [directory('/d')] }, '/', () => false)
    expect(reads).toBe(1)
  })
})
