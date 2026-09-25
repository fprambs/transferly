import type { Collection } from '@/lib/api'

/** A collection with the path that leads to it, for pickers. */
export interface CollectionOption {
  /** Collection identifier. */
  id: string
  /** Names from the top down, joined with a slash. */
  label: string
}

/** Builds the path of a collection, stopping at the top or at an unknown parent. */
function pathOf(collection: Collection, byId: Map<string, Collection>): string {
  const parent = collection.parentId === undefined ? undefined : byId.get(collection.parentId)
  return parent === undefined ? collection.name : `${pathOf(parent, byId)} / ${collection.name}`
}

/** Collects a collection and every collection below it. */
function subtreeIds(id: string, collections: readonly Collection[]): string[] {
  return collections.filter((collection) => collection.parentId === id).reduce((ids, child) => [...ids, ...subtreeIds(child.id, collections)], [id])
}

/**
 * Lists every collection with its full path, sorted by path.
 *
 * @param collections - All collections of the workspace.
 * @param excludeId - A collection to leave out together with everything below it, so it cannot be moved into itself.
 */
export function collectionOptions(collections: readonly Collection[], excludeId?: string): CollectionOption[] {
  const byId = new Map(collections.map((collection) => [collection.id, collection]))
  const excluded = new Set(excludeId === undefined ? [] : subtreeIds(excludeId, collections))
  return collections
    .filter((collection) => !excluded.has(collection.id))
    .map((collection) => ({ id: collection.id, label: pathOf(collection, byId) }))
    .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: 'base' }))
}
