import { ChevronRight, Ellipsis, FileInput, Folder, FolderPlus, PanelLeftClose, Plus, Zap } from 'lucide-react'
import { useMemo, useState, type KeyboardEvent, type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Tip } from '@/components/ui/tip'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { Collection, Site } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useDialogsStore } from '@/state/dialogs-store'

import { useWorkspace } from '../workspace/use-workspace'

import { CollectionDialog, type CollectionTarget } from './CollectionDialog'
import { DeleteCollectionDialog } from './DeleteCollectionDialog'
import { DeleteSiteDialog } from './DeleteSiteDialog'
import { ImportDialog } from './ImportDialog'
import { SiteDialog, type SiteTarget } from './SiteDialog'
import { buildTree, type CollectionNode, type TreeNode } from './tree'
import { useOpenConnections } from './use-open-connections'
import { useSitesData } from './use-sites-data'

/** Indentation of each nesting level, in pixels. */
const indentPerLevel = 14

/** What can be done to the rows of the tree. */
interface CollectionActions {
  /** Key of the row that is selected, if any. */
  selected: string | null
  /** Called when a row is selected. */
  onSelect: (key: string) => void
  /** Called to open a saved site or a quick connection. */
  onOpen: (siteId: string) => void
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

/** Selects a row with Enter or Space, when the row itself has focus. */
function onSelectKey(event: KeyboardEvent<HTMLElement>, select: () => void): void {
  if (event.target === event.currentTarget && (event.key === 'Enter' || event.key === ' ')) {
    event.preventDefault()
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

/** One row of the sites tree: a collection, a saved site or a quick connection. */
function Row({ node, actions }: RowProps) {
  const { t } = useTranslation('shell')
  const { t: sites } = useTranslation('sites')
  const selected = actions.selected === rowKey(node)
  const select = () => {
    actions.onSelect(rowKey(node))
    if (node.kind !== 'collection') {
      actions.onOpen(node.id)
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
        onClick={(event) => {
          if (inRow(event)) {
            select()
            actions.onToggle(node.id)
          }
        }}
        onKeyDown={(event) => onCollectionKey(event, node, actions.onToggle)}
        className={cn(base, 'group cursor-pointer font-medium')}
        style={style}
      >
        <ChevronRight className={cn('size-3 shrink-0 text-faint transition-transform rtl:-scale-x-100', node.expanded && 'rotate-90 rtl:rotate-90')} />
        <Folder className="size-[15px] shrink-0 text-muted-foreground" />
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
        onClick={select}
        onKeyDown={(event) => onSelectKey(event, select)}
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
      onClick={(event) => inRow(event) && select()}
      onKeyDown={(event) => onSelectKey(event, select)}
      className={cn(base, 'group')}
      style={style}
    >
      <span className="w-3 shrink-0" />
      {actions.connected.has(node.id) ? <ConnectedDot /> : <span className="mx-[3px] size-2 shrink-0 rounded-full bg-muted-foreground/50" />}
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
    <span className="mx-[3px] size-2 shrink-0 rounded-full bg-ok ring-3 ring-ok/20">
      <span className="sr-only">{t(($) => $.sidebar.connected)}</span>
    </span>
  )
}

/** Returns the open collections after opening or closing one. */
function toggled(open: ReadonlySet<string>, id: string): ReadonlySet<string> {
  return new Set(open.has(id) ? [...open].filter((item) => item !== id) : [...open, id])
}

/** Props of {@link SitesSidebar}. */
export interface SitesSidebarProps {
  /** Called with the identifier of the saved site or quick connection the user opens. */
  onOpen: (siteId: string) => void
  /** Hides the sites panel; without it, the sidebar offers no way to hide itself, as when it is shown in a sheet. */
  onHide?: () => void
  /** Closes the connections of a site, offered in the menu of a connected site. */
  onDisconnect?: (siteId: string) => void
}

/** Sidebar of the shell: quick connect and the tree of collections and sites. */
export function SitesSidebar({ onOpen, onHide, onDisconnect = () => undefined }: SitesSidebarProps) {
  const { t } = useTranslation('shell')
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
  const rows = useMemo(() => buildTree({ collections, sites, quick, expanded }), [collections, sites, quick, expanded])
  const [selected, setSelected] = useState<string | null>(null)
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
  }

  return (
    <nav aria-label={t(($) => $.sidebar.label)} className="flex h-full min-h-0 w-full flex-col bg-card">
      <div className="flex items-center justify-between px-4 pt-3.5 pb-2">
        <h2 className="text-[11px] font-medium tracking-widest text-muted-foreground uppercase">{t(($) => $.sidebar.sites)}</h2>
        <div className="flex gap-1">
          <Button variant="outline" size="icon-xs" aria-label={t(($) => $.sidebar.newCollection)} onClick={() => setTarget({ mode: 'create' })}>
            <FolderPlus />
          </Button>
          <Button variant="outline" size="icon-xs" aria-label={t(($) => $.sidebar.importSites)} onClick={() => setImporting(true)}>
            <FileInput />
          </Button>
          <Button variant="outline" size="icon-xs" aria-label={t(($) => $.sidebar.newSite)} onClick={() => setSiteTarget({ mode: 'create' })}>
            <Plus />
          </Button>
          {onHide !== undefined && (
            <Tip label={t(($) => $.sidebar.hide)} shortcut="Ctrl+B">
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
          <span className="flex-1 text-start">{t(($) => $.sidebar.quickConnect)}</span>
          <kbd className="rounded-md border px-1.5 font-mono text-[11px] text-muted-foreground">⌘⇧K</kbd>
        </Button>
      </div>
      <div role="tree" aria-label={t(($) => $.sidebar.sites)} className="flex min-h-0 flex-col gap-px overflow-y-auto px-2">
        {rows.map((node) => (
          <Row key={`${node.kind}-${node.id}`} node={node} actions={actions} />
        ))}
      </div>
      {rows.length === 0 && <p className="px-4 text-sm text-muted-foreground">{t(($) => $.sidebar.empty)}</p>}
      {workspace !== undefined && (
        <>
          <ImportDialog open={importing} workspaceId={workspace.id} onClose={() => setImporting(false)} />
          <CollectionDialog target={target} workspaceId={workspace.id} collections={collections} onClose={() => setTarget(null)} />
          <DeleteCollectionDialog collection={deleting} workspaceId={workspace.id} onClose={() => setDeleting(null)} />
          <SiteDialog target={siteTarget} workspaceId={workspace.id} collections={collections} onClose={() => setSiteTarget(null)} />
          <DeleteSiteDialog site={deletingSite} workspaceId={workspace.id} onClose={() => setDeletingSite(null)} />
        </>
      )}
    </nav>
  )
}
