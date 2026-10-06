/** A column the entries of a folder can be sorted by. */
export type SortKey = 'name' | 'size' | 'type' | 'modified'

/** The order of the entries of a folder: the column and its direction. */
export interface EntrySort {
  /** The column. */
  key: SortKey
  /** Ascending or descending. */
  direction: 'asc' | 'desc'
}

/** How folders open: the newest entries first. */
export const defaultSort: EntrySort = { key: 'modified', direction: 'desc' }

/** The direction a column takes when it is chosen: names and types from A to Z, sizes from the largest and times from the newest. */
export function firstDirection(key: SortKey): EntrySort['direction'] {
  return key === 'name' || key === 'type' ? 'asc' : 'desc'
}

/** The order after a click on the header of a column: the same column flips its direction, another one starts in its own. */
export function nextSort(current: EntrySort, key: SortKey): EntrySort {
  return current.key === key ? { key, direction: current.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: firstDirection(key) }
}
