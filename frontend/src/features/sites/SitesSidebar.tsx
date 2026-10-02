import { ChevronRight, Ellipsis, FileInput, FileLock2, Folder, FolderPlus, PanelLeftClose, Plus, Zap } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type DragEvent, type KeyboardEvent, type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { BrandIcon } from '@/components/brand/BrandIcon'
import { Illustration } from '@/components/brand/Illustration'
import { ProtocolIcon } from '@/components/brand/ProtocolIcon'
import { Button } from '@/components/ui/button'
import { Tip } from '@/components/ui/tip'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { Collection, Site, Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { formatShortcut } from '@/lib/shortcuts'
import { cn } from '@/lib/utils'
import { useDialogsStore } from '@/state/dialogs-store'

import { canManageSites } from '../teams/roles'
import { useWorkspace } from '../workspace/use-workspace'

import { CollectionDialog, type CollectionTarget } from './CollectionDialog'
import { DeleteCollectionDialog } from './DeleteCollectionDialog'
import { DeleteSiteDialog } from './DeleteSiteDialog'
import { ExportDialog } from './ExportDialog'
import { ImportDialog } from './ImportDialog'
import { SiteDialog, type SiteTarget } from './SiteDialog'
import { colorFill } from './site-colors'
import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { buildTree, type CollectionNode, type TreeNode } from './tree'
import { canDrop, treeDragType, type TreeDrag } from './tree-drag'
import { useOpenConnections } from './use-open-connections'
import { sitesKeys, useSitesData } from './use-sites-data'

/** Tells whether the user may export the sites of a workspace with their secrets: its admins and owner only (ADR-0017). */
function canExport(workspace: Workspace | undefined): boolean {
  return workspace?.role === 'admin' || workspace?.role === 'owner'
}

/** Indentation of each nesting level, in pixels. */
const indentPerLevel = 14

/** What can be done to the rows of the tree. */
interface CollectionActions {
  /** Key of the row that is selected, if any. */
  selected: string | null
  /** Called when a row is selected. */
  onSelect: (key: string) => void
  /** Called to open a saved site or a quick connection, in a new tab when asked to. */
  onOpen: (siteId: string, newTab?: boolean) => void
  /** Called when a collection is opened or closed. */
  onToggle: (id: string) => void
  /** Called to create a collection inside another. */
  onCreateIn: (parentId: string) => void
  /** Called to rename or move a collection. */
  onEdit: (id: string) => void
  /** Called to delete a collection. */
  onDelete: (id: string) => void
  /** Called to create a site inside a collection. */
  onCreateSiteIn: (collectionId: string) => void
  /** Called to edit a saved site. */
  onEditSite: (id: string) => void
  /** Called to delete a saved site. */
  onDeleteSite: (id: string) => void
  /** Sites and quick connections with connections open, marked in green (4.11). */
  connected: ReadonlySet<string>
  /** Called to close the connections to a site. */
  onDisconnect: (id: string) => void
  /** Whether saved sites and collections can be dragged to another collection (4.14h). */
  draggable: boolean
  /** Called when the user starts dragging a row. */
  onDragRow: (drag: TreeDrag, data: DataTransfer) => void
  /** Called when a drag of a row ends, dropped or not. */
  onDragEnd: () => void
  /** Collection the dragged row would go into, marked while it is over it. */
  dropTarget: string | null | undefined
}

/** Props of {@link Row}. */
interface RowProps {
  /** The row to draw. */
  node: TreeNode
  /** What the row's collection can do. */
  actions: CollectionActions
}

/** Whether a click happened on the row itself: a click in the row's menu, drawn elsewhere in a portal, still bubbles to the row through React. */
function inRow(event: MouseEvent<HTMLElement>): boolean {
  return event.currentTarget.contains(event.target as Node)
}

/** Returns the key that identifies a row in the selection. */
function rowKey(node: TreeNode): string {
  return `${node.kind}-${node.id}`
}

/** Opens a site with Enter and selects it with Space, when the row itself has focus (4.14f). */
function onSiteKey(event: KeyboardEvent<HTMLElement>, select: () => void, open: () => void): void {
  if (event.target !== event.currentTarget || (event.key !== 'Enter' && event.key !== ' ')) {
    return
  }
  event.preventDefault()
  if (event.key === 'Enter') {
    open()
  } else {
    select()
  }
}

/** Opens or closes a collection from the keyboard: Enter and Space toggle, the arrows open and close. Only for the row itself. */
function onCollectionKey(event: KeyboardEvent<HTMLElement>, node: CollectionNode, onToggle: (id: string) => void): void {
  if (event.target !== event.currentTarget) {
    return
  }
  const toggles =
    event.key === 'Enter' ||
    event.key === ' ' ||
    (event.key === 'ArrowRight' && !node.expanded) ||
    (event.key === 'ArrowLeft' && node.expanded)
  if (toggles) {
    event.preventDefault()
    onToggle(node.id)
  }
}

/**
 * One row of the sites tree: a collection, a saved site or a quick connection. A click selects a site and a double click or Enter connects
 * it, as in FileZilla (4.14f); Ctrl with the double click, or a middle click, opens it in a new tab. A click opens or closes a collection.
 */
function Row({ node, actions }: RowProps) {
  const { t } = useTranslation('shell')
  const { t: sites } = useTranslation('sites')
  const selected = actions.selected === rowKey(node)
  const select = (): void => actions.onSelect(rowKey(node))
  const open = (newTab = false): void => {
    select()
    actions.onOpen(node.id, newTab)
  }
  const click = (event: MouseEvent<HTMLElement>): void => {
    if (inRow(event)) {
      select()
    }
  }
  const doubleClick = (event: MouseEvent<HTMLElement>): void => {
    if (inRow(event)) {
      open(event.ctrlKey || event.metaKey)
    }
  }
  const middle = (event: MouseEvent<HTMLElement>): void => {
    if (event.button === 1 && inRow(event)) {
      event.preventDefault()
      open(true)
    }
  }
  const style = { paddingInlineStart: `${node.depth * indentPerLevel + 8}px` }
  const base =
    'flex h-[30px] w-full cursor-pointer items-center gap-2 rounded-md pe-2 text-start text-[13px] outline-none hover:bg-accent hover:[&_span]:text-foreground focus-visible:ring-2 focus-visible:ring-ring aria-selected:bg-primary/15 aria-selected:font-medium aria-selected:[&_span]:text-foreground'

  if (node.kind === 'collection') {
    return (
      <div
        role="treeitem"
        aria-expanded={node.expanded}
        aria-level={node.depth + 1}
        aria-selected={selected}
        tabIndex={0}
        data-drop-target={node.id}
        draggable={actions.draggable}
        onDragStart={(event) => actions.onDragRow({ kind: 'collection', id: node.id }, event.dataTransfer)}
        onDragEnd={actions.onDragEnd}
        onClick={(event) => {
          if (inRow(event)) {
            select()
            actions.onToggle(node.id)
          }
        }}
        onKeyDown={(event) => onCollectionKey(event, node, actions.onToggle)}
        className={cn(base, 'group cursor-pointer font-medium', actions.dropTarget === node.id && 'bg-primary/10 ring-2 ring-primary/60')}
        style={style}
      >
        <ChevronRight className={cn('size-3 shrink-0 text-faint transition-transform rtl:-scale-x-100', node.expanded && 'rotate-90 rtl:rotate-90')} />
        <Folder className="size-[15px] shrink-0 text-accent-text" />
        <span className="min-w-0 flex-1 truncate">{node.name}</span>
        <span className="font-mono text-[11px] text-muted-foreground">{node.siteCount}</span>
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label={sites(($) => $.collection.actions, { name: node.name })}
            onClick={(event) => event.stopPropagation()}
            className="flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 group-focus-within:opacity-100 group-hover:opacity-100 hover:bg-background data-popup-open:bg-background data-popup-open:opacity-100"
          >
            <Ellipsis className="size-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
            <DropdownMenuItem onClick={() => actions.onCreateSiteIn(node.id)}>{sites(($) => $.site.newHere)}</DropdownMenuItem>
            <DropdownMenuItem onClick={() => actions.onCreateIn(node.id)}>{sites(($) => $.collection.newSub)}</DropdownMenuItem>
            <DropdownMenuItem onClick={() => actions.onEdit(node.id)}>{sites(($) => $.collection.edit)}</DropdownMenuItem>
            <DropdownMenuItem onClick={() => actions.onDelete(node.id)}>{sites(($) => $.collection.delete)}</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    )
  }
  if (node.kind === 'quick') {
    return (
      <div
        role="treeitem"
        aria-level={1}
        aria-selected={selected}
        tabIndex={0}
        onClick={click}
        onDoubleClick={doubleClick}
        onAuxClick={middle}
        onKeyDown={(event) => onSiteKey(event, select, open)}
        className={cn(base, 'italic')}
        style={style}
      >
        <span className="w-3 shrink-0" />
        <Zap className="size-[15px] shrink-0 text-warn" />
        <span className="min-w-0 flex-1 truncate">{node.name}</span>
        {actions.connected.has(node.id) && <ConnectedDot />}
        <span className="text-[11px] text-muted-foreground">{t(($) => $.sidebar.notSaved)}</span>
      </div>
    )
  }
  return (
    <div
      role="treeitem"
      aria-level={node.depth + 1}
      aria-selected={selected}
      tabIndex={0}
      data-drop-target={node.collectionId ?? ''}
      draggable={actions.draggable}
      onDragStart={(event) => actions.onDragRow({ kind: 'site', id: node.id }, event.dataTransfer)}
      onDragEnd={actions.onDragEnd}
      onClick={click}
      onDoubleClick={doubleClick}
      onAuxClick={middle}
      onKeyDown={(event) => onSiteKey(event, select, open)}
      className={cn(base, 'group')}
      style={style}
    >
      <span className="flex w-3 shrink-0 justify-center">
        <span data-color={node.color} className={cn('h-4 w-[3px] rounded-full', colorFill(node.color))} />
      </span>
      {actions.connected.has(node.id) ? <ConnectedDot /> : <span className="mx-[3px] size-2 shrink-0 rounded-full bg-muted-foreground/50" />}
      <ProtocolIcon transport={node.transport} className="size-[15px]" />
      <span className="min-w-0 flex-1 truncate">{node.name}</span>
      <span className="max-w-24 truncate font-mono text-[11px] text-muted-foreground">{node.host}</span>
      <DropdownMenu>
        <DropdownMenuTrigger
          aria-label={sites(($) => $.site.actions, { name: node.name })}
          onClick={(event) => event.stopPropagation()}
          className="flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 group-focus-within:opacity-100 group-hover:opacity-100 hover:bg-background data-popup-open:bg-background data-popup-open:opacity-100"
        >
          <Ellipsis className="size-3.5" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-40">
          <DropdownMenuItem onClick={() => actions.onOpen(node.id, true)}>{sites(($) => $.site.openInNewTab)}</DropdownMenuItem>
          {actions.connected.has(node.id) && <DropdownMenuItem onClick={() => actions.onDisconnect(node.id)}>{sites(($) => $.site.disconnect)}</DropdownMenuItem>}
          <DropdownMenuItem onClick={() => actions.onEditSite(node.id)}>{sites(($) => $.site.edit)}</DropdownMenuItem>
          <DropdownMenuItem onClick={() => actions.onDeleteSite(node.id)}>{sites(($) => $.site.delete)}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

/** The green dot of a connected site, with its meaning for screen readers. */
function ConnectedDot() {
  const { t } = useTranslation('shell')
  return (
    <span className="relative mx-[3px] size-2 shrink-0 rounded-full bg-ok ring-3 ring-ok/20 duration-300 animate-in zoom-in-50">
      <span aria-hidden="true" className="absolute inset-0 rounded-full bg-ok motion-safe:animate-[ping_1s_var(--ease-fluid)_1]" />
      <span className="sr-only">{t(($) => $.sidebar.connected)} </span>
    </span>
  )
}

/** Returns the open collections after opening or closing one. */
function toggled(open: ReadonlySet<string>, id: string): ReadonlySet<string> {
  return new Set(open.has(id) ? [...open].filter((item) => item !== id) : [...open, id])
}

/** Props of {@link SitesSidebar}. */
export interface SitesSidebarProps {
  /** Called with the identifier of the saved site or quick connection the user opens, in a new tab when asked to. */
  onOpen: (siteId: string, newTab?: boolean) => void
  /** Hides the sites panel; without it, the sidebar offers no way to hide itself, as when it is shown in a sheet. */
  onHide?: () => void
  /** Closes the connections of a site, offered in the menu of a connected site. */
  onDisconnect?: (siteId: string) => void
}

/**
 * Sidebar of the shell: quick connect and the tree of collections and sites. Whoever may manage the sites drags a saved site or a collection
 * into another collection, or to the empty room of the tree for the top level (4.14h); the tree stays in alphabetical order.
 */
export function SitesSidebar({ onOpen, onHide, onDisconnect = () => undefined }: SitesSidebarProps) {
  const { t } = useTranslation('shell')
  const { t: sitesT } = useTranslation('sites')
  const workspace = useWorkspace()
  const setQuickConnectOpen = useDialogsStore((state) => state.setQuickConnectOpen)
  const { collections, sites, quick } = useSitesData(workspace?.id)
  const connected = useOpenConnections(workspace?.id)
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [target, setTarget] = useState<CollectionTarget | null>(null)
  const [deleting, setDeleting] = useState<Collection | null>(null)
  const [siteTarget, setSiteTarget] = useState<SiteTarget | null>(null)
  const [deletingSite, setDeletingSite] = useState<Site | null>(null)
  const [importing, setImporting] = useState(false)
  const [exporting, setExporting] = useState(false)
  const rows = useMemo(() => buildTree({ collections, sites, quick, expanded }), [collections, sites, quick, expanded])
  const [selected, setSelected] = useState<string | null>(null)
  const api = useApi()
  const queryClient = useQueryClient()
  const [dragging, setDragging] = useState<TreeDrag | null>(null)
  const [over, setOver] = useState<string | null | undefined>(undefined)
  const [moveFailure, setMoveFailure] = useState<SiteErrorKey | null>(null)
  const endDrag = (): void => {
    setDragging(null)
    setOver(undefined)
  }
  const targetOf = (event: DragEvent<HTMLElement>): string | null => (event.target as HTMLElement).closest('[data-drop-target]')?.getAttribute('data-drop-target') || null
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
    if (drag === null || !canDrop(drag, target, collections, sites)) {
      return
    }
    setMoveFailure(null)
    const id = (workspace as Workspace).id
    try {
      if (drag.kind === 'site') {
        await api.sites.move(id, drag.id, target ?? undefined)
      } else {
        await api.collections.update(id, drag.id, target === null ? { moveToTop: true } : { parentId: target })
      }
      if (target !== null) {
        setExpanded((open) => new Set([...open, target]))
      }
    } catch (error) {
      setMoveFailure(siteErrorKeyOf(error))
    }
    await Promise.all([queryClient.invalidateQueries({ queryKey: sitesKeys.sites(id) }), queryClient.invalidateQueries({ queryKey: sitesKeys.collections(id) })])
  }
  const actions: CollectionActions = {
    selected,
    onSelect: setSelected,
    onOpen,
    onToggle: (id) => setExpanded((open) => toggled(open, id)),
    onCreateIn: (parentId) => {
      setExpanded((open) => new Set([...open, parentId]))
      setTarget({ mode: 'create', parentId })
    },
    onEdit: (id) => setTarget({ mode: 'edit', collection: collections.find((collection) => collection.id === id) as Collection }),
    onDelete: (id) => setDeleting(collections.find((collection) => collection.id === id) as Collection),
    onCreateSiteIn: (collectionId) => {
      setExpanded((open) => new Set([...open, collectionId]))
      setSiteTarget({ mode: 'create', collectionId })
    },
    onEditSite: (id) => setSiteTarget({ mode: 'edit', site: sites.find((site) => site.id === id) as Site }),
    onDeleteSite: (id) => setDeletingSite(sites.find((site) => site.id === id) as Site),
    connected,
    onDisconnect,
    draggable: workspace !== undefined && canManageSites(workspace.role),
    onDragRow: (drag, data) => {
      data.setData(treeDragType, drag.id)
      data.effectAllowed = 'move'
      setDragging(drag)
    },
    onDragEnd: endDrag,
    dropTarget: over,
  }

  return (
    <nav aria-label={t(($) => $.sidebar.label)} className="@container/sites flex h-full min-h-0 w-full flex-col bg-card">
      <div className="flex items-center justify-between px-4 pt-3.5 pb-2">
        <h2 className="flex items-center gap-2 text-[11px] font-medium tracking-widest text-muted-foreground uppercase">
          <BrandIcon name="sites" className="size-3.5" />
          {t(($) => $.sidebar.sites)}
        </h2>
        <div className="flex gap-0.5">
          <Tip label={t(($) => $.sidebar.newSite)}>
            <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.sidebar.newSite)} onClick={() => setSiteTarget({ mode: 'create' })}>
              <Plus />
            </Button>
          </Tip>
          <DropdownMenu>
            <DropdownMenuTrigger
              aria-label={t(($) => $.sidebar.more)}
              className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
            >
              <Ellipsis aria-hidden="true" className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem onClick={() => setTarget({ mode: 'create' })}>
                <FolderPlus aria-hidden="true" />
                {t(($) => $.sidebar.newCollection)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setImporting(true)}>
                <FileInput aria-hidden="true" />
                {t(($) => $.sidebar.importSites)}
              </DropdownMenuItem>
              {canExport(workspace) && (
                <DropdownMenuItem onClick={() => setExporting(true)}>
                  <FileLock2 aria-hidden="true" />
                  {t(($) => $.sidebar.exportSites)}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          {onHide !== undefined && (
            <Tip label={t(($) => $.sidebar.hide)} shortcut="Mod+B">
              <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.sidebar.hide)} onClick={onHide}>
                <PanelLeftClose />
              </Button>
            </Tip>
          )}
        </div>
      </div>
      <div className="px-2 pb-2.5">
        <Button variant="outline" className="h-8 w-full justify-start gap-2 bg-muted/40" onClick={() => setQuickConnectOpen(true)}>
          <Zap className="text-warn" />
          <span className="min-w-0 flex-1 truncate text-start">{t(($) => $.sidebar.quickConnect)}</span>
          <kbd className="shrink-0 rounded-md border px-1.5 font-mono text-[11px] text-muted-foreground @max-[18rem]/sites:hidden">{formatShortcut('Mod+Shift+K')}</kbd>
        </Button>
      </div>
      {moveFailure !== null && (
        <p role="alert" className="mx-2 mb-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-danger-text">
          {sitesT(($) => $.errors[moveFailure])}
        </p>
      )}
      {rows.length === 0 && (
        <div className="flex flex-col items-center gap-3 px-4 py-6 text-center">
          <Illustration scene="sites" className="h-16 w-24" />
          <div className="flex flex-col gap-1">
            <p className="text-sm font-medium">{t(($) => $.sidebar.empty)}</p>
            <p className="text-xs text-muted-foreground">{t(($) => $.sidebar.emptyHint)}</p>
          </div>
          {workspace !== undefined && canManageSites(workspace.role) && (
            <div className="flex w-full flex-col gap-1.5">
              <Button size="sm" onClick={() => setSiteTarget({ mode: 'create' })}>
                <Plus />
                {t(($) => $.sidebar.emptyAdd)}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setImporting(true)}>
                <FileInput />
                {t(($) => $.sidebar.emptyImport)}
              </Button>
            </div>
          )}
        </div>
      )}
      <div
        role="tree"
        aria-label={t(($) => $.sidebar.sites)}
        onDragOver={dragOver}
        onDragLeave={(event) => !event.currentTarget.contains(event.relatedTarget as Node | null) && setOver(undefined)}
        onDrop={(event) => void drop(event)}
        className={cn('flex min-h-0 flex-1 flex-col gap-px overflow-y-auto rounded-md px-2 pb-4', over === null && 'bg-primary/5 ring-2 ring-primary/40 ring-inset')}
      >
        {rows.map((node) => (
          <Row key={`${node.kind}-${node.id}`} node={node} actions={actions} />
        ))}
      </div>
      {workspace !== undefined && (
        <>
          <ImportDialog open={importing} workspaceId={workspace.id} onClose={() => setImporting(false)} />
          <ExportDialog open={exporting} workspaceId={workspace.id} onClose={() => setExporting(false)} />
          <CollectionDialog target={target} workspaceId={workspace.id} collections={collections} onClose={() => setTarget(null)} />
          <DeleteCollectionDialog collection={deleting} workspaceId={workspace.id} onClose={() => setDeleting(null)} />
          <SiteDialog target={siteTarget} workspaceId={workspace.id} collections={collections} onClose={() => setSiteTarget(null)} />
          <DeleteSiteDialog site={deletingSite} workspaceId={workspace.id} onClose={() => setDeletingSite(null)} />
        </>
      )}
    </nav>
  )
}
