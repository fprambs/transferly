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

/** Lists every collection with its full path, sorted by path. */
export function collectionOptions(collections: readonly Collection[]): CollectionOption[] {
  const byId = new Map(collections.map((collection) => [collection.id, collection]))
  return collections
    .map((collection) => ({ id: collection.id, label: pathOf(collection, byId) }))
    .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: 'base' }))
}
