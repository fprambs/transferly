/** Data type that carries a dragged file between the panes of the explorer. */
export const entryDragType = 'application/x-sentinel-entry'

/** A file being dragged from a pane, and where it lives. */
export interface DraggedEntry {
  /** Workspace of the site the file lives on. */
  workspaceId: string
  /** Saved site or quick connection the file lives on. */
  siteId: string
  /** Absolute path of the file. */
  path: string
}

/** Puts a dragged file in the drag data. */
export function writeDrag(data: DataTransfer, entry: DraggedEntry): void {
  data.setData(entryDragType, JSON.stringify(entry))
  data.effectAllowed = 'copy'
}

/** Tells whether the drag carries a file of the explorer; a drag event may have no data at all. */
export function isEntryDrag(data: DataTransfer | null): boolean {
  return data?.types.includes(entryDragType) ?? false
}

/** Reads the file being dragged, or returns `null` when the drag carries none or its data is not well formed. */
export function readDrag(data: DataTransfer): DraggedEntry | null {
  const raw = data.getData(entryDragType)
  if (raw === '') {
    return null
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  const entry = parsed as Partial<Record<keyof DraggedEntry, unknown>> | null
  if (typeof entry?.workspaceId === 'string' && typeof entry.siteId === 'string' && typeof entry.path === 'string') {
    return { workspaceId: entry.workspaceId, siteId: entry.siteId, path: entry.path }
  }
  return null
}
