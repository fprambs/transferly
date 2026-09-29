import { describe, expect, it } from 'vitest'

import type { Collection, Site } from '@/lib/api'

import { canDrop } from './tree-drag'

/** Collections: a holds b, which holds c; d is at the top. */
const collections: Collection[] = [
  { id: 'a', name: 'A' },
  { id: 'b', name: 'B', parentId: 'a' },
  { id: 'c', name: 'C', parentId: 'b' },
  { id: 'd', name: 'D' },
]

/** A site in b and one at the top. */
const sites = [{ id: 's1', collectionId: 'b' }, { id: 's2' }] as Site[]

/**
 * Covers {@link canDrop} (4.14h).
 *
 * Scenarios:
 * - a site goes to any collection or the top level except where it is
 * - a collection goes anywhere except where it is, into itself or into one of its own collections
 */
describe('canDrop', () => {
  it('moves a site anywhere but where it is', () => {
    expect(canDrop({ kind: 'site', id: 's1' }, 'b', collections, sites)).toBe(false)
    expect(canDrop({ kind: 'site', id: 's1' }, null, collections, sites)).toBe(true)
    expect(canDrop({ kind: 'site', id: 's2' }, null, collections, sites)).toBe(false)
    expect(canDrop({ kind: 'site', id: 's2' }, 'd', collections, sites)).toBe(true)
  })

  it('never moves a collection into itself or its own collections', () => {
    expect(canDrop({ kind: 'collection', id: 'b' }, 'a', collections, sites)).toBe(false)
    expect(canDrop({ kind: 'collection', id: 'b' }, 'b', collections, sites)).toBe(false)
    expect(canDrop({ kind: 'collection', id: 'a' }, 'c', collections, sites)).toBe(false)
    expect(canDrop({ kind: 'collection', id: 'b' }, 'd', collections, sites)).toBe(true)
    expect(canDrop({ kind: 'collection', id: 'b' }, null, collections, sites)).toBe(true)
    expect(canDrop({ kind: 'collection', id: 'a' }, null, collections, sites)).toBe(false)
  })
})
