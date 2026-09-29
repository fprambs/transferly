import type { Collection, Site } from '@/lib/api'

/** Type of the drag data of a row of the sites tree, which only the tree reads. */
export const treeDragType = 'application/x-sentinel-tree'

/** A row of the sites tree being dragged (4.14h). */
export interface TreeDrag {
  /** Whether a saved site or a collection is dragged. */
  kind: 'site' | 'collection'
  /** Its identifier. */
  id: string
}

/**
 * Whether a dragged row may be dropped into a collection, or at the top level for `null`: not where it already is, and a collection never
 * into itself or one of its own collections.
 */
export function canDrop(drag: TreeDrag, target: string | null, collections: readonly Collection[], sites: readonly Site[]): boolean {
  if (drag.kind === 'site') {
    return target !== (sites.find((site) => site.id === drag.id)?.collectionId ?? null)
  }
  const parents = new Map(collections.map((collection) => [collection.id, collection.parentId ?? null]))
  if (target === (parents.get(drag.id) ?? null)) {
    return false
  }
  for (let at = target; at !== null; at = parents.get(at) ?? null) {
    if (at === drag.id) {
      return false
    }
  }
  return true
}
