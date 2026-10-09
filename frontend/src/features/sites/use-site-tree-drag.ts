import { useQueryClient } from '@tanstack/react-query'
import { useState, type DragEvent } from 'react'

import type { Collection, Site, Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { canDrop, treeDragType, type TreeDrag } from './tree-drag'
import { sitesKeys } from './use-sites-data'

/** Inputs for moving rows of a workspace's site tree. */
export interface SiteTreeDragOptions {
  /** Workspace whose rows are shown, once it has loaded. */
  workspace: Workspace | undefined
  /** Collections used to reject cycles and invalid drops. */
  collections: Collection[]
  /** Sites used to resolve their current parent. */
  sites: Site[]
  /** Expands the destination after a successful move. */
  onMoved: (collectionId: string) => void
}

/** State and operations of one drag interaction, without tree rendering or dialog state. */
export interface SiteTreeDrag {
  /** Collection under the pointer; null is the top level and undefined means no drop target. */
  over: string | null | undefined
  /** Guided failure of the last accepted move. */
  moveFailure: SiteErrorKey | null
  /** Starts dragging a site or collection. */
  beginDrag: (drag: TreeDrag, data: DataTransfer) => void
  /** Clears the row and its drop target when dragging ends. */
  endDrag: () => void
  /** Accepts only valid moves and updates the highlighted destination. */
  dragOver: (event: DragEvent<HTMLElement>) => void
  /** Clears the highlighted destination after leaving the tree rather than one of its child rows. */
  dragLeave: (event: DragEvent<HTMLElement>) => void
  /** Moves the accepted row and reconciles both site and collection queries. */
  drop: (event: DragEvent<HTMLElement>) => Promise<void>
}

/** Resolves a row destination or the empty tree area for a top-level move. */
function targetOf(event: DragEvent<HTMLElement>): string | null {
  return (event.target as HTMLElement).closest('[data-drop-target]')?.getAttribute('data-drop-target') || null
}

/** Coordinates site-tree movement, validation, feedback and cache reconciliation. */
export function useSiteTreeDrag({ workspace, collections, sites, onMoved }: SiteTreeDragOptions): SiteTreeDrag {
  const api = useApi()
  const queryClient = useQueryClient()
  const [dragging, setDragging] = useState<TreeDrag | null>(null)
  const [over, setOver] = useState<string | null | undefined>(undefined)
  const [moveFailure, setMoveFailure] = useState<SiteErrorKey | null>(null)
  const endDrag = (): void => { setDragging(null); setOver(undefined) }
  const beginDrag = (drag: TreeDrag, data: DataTransfer): void => {
    data.setData(treeDragType, drag.id)
    data.effectAllowed = 'move'
    setDragging(drag)
  }
  const dragOver = (event: DragEvent<HTMLElement>): void => {
    const target = targetOf(event)
    if (dragging === null || !canDrop(dragging, target, collections, sites)) {
      setOver(undefined)
      return
    }
    event.preventDefault()
    event.dataTransfer.dropEffect = 'move'
    setOver(target)
  }
  const drop = async (event: DragEvent<HTMLElement>): Promise<void> => {
    event.preventDefault()
    const drag = dragging
    const target = targetOf(event)
    endDrag()
    if (drag === null || !canDrop(drag, target, collections, sites)) return
    setMoveFailure(null)
    const id = (workspace as Workspace).id
    try {
      if (drag.kind === 'site') await api.sites.move(id, drag.id, target ?? undefined)
      else await api.collections.update(id, drag.id, target === null ? { moveToTop: true } : { parentId: target })
      if (target !== null) onMoved(target)
    } catch (error) {
      setMoveFailure(siteErrorKeyOf(error))
    }
    await Promise.all([queryClient.invalidateQueries({ queryKey: sitesKeys.sites(id) }), queryClient.invalidateQueries({ queryKey: sitesKeys.collections(id) })])
  }
  const dragLeave = (event: DragEvent<HTMLElement>): void => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(undefined)
  }
  return { over, moveFailure, beginDrag, endDrag, dragOver, dragLeave, drop }
}
