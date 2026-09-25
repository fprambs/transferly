import { describe, expect, it } from 'vitest'

import { collectionOptions } from './collection-paths'

/**
 * Covers {@link collectionOptions}.
 *
 * Scenarios:
 * - nested collections carry their full path and the list is sorted by path
 * - a collection with an unknown parent is labelled by its own name
 * - no collections give no options
 */
describe('collectionOptions', () => {
  it('builds sorted paths', () => {
    const options = collectionOptions([
      { id: 'c3', name: 'Staging', parentId: 'c2' },
      { id: 'c1', name: 'Home' },
      { id: 'c2', name: 'Work' },
    ])

    expect(options).toEqual([
      { id: 'c1', label: 'Home' },
      { id: 'c2', label: 'Work' },
      { id: 'c3', label: 'Work / Staging' },
    ])
  })

  it('labels an orphan by its own name', () => {
    expect(collectionOptions([{ id: 'c1', name: 'Lost', parentId: 'gone' }])).toEqual([{ id: 'c1', label: 'Lost' }])
  })

  it('is empty without collections', () => {
    expect(collectionOptions([])).toEqual([])
  })
})
