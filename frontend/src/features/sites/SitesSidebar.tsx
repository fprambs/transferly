import { ChevronRight, Ellipsis, Folder, FolderPlus, Plus, Zap } from 'lucide-react'
import { useMemo, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { Collection } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useDialogsStore } from '@/state/dialogs-store'

import { useWorkspace } from '../workspace/use-workspace'

import { CollectionDialog, type CollectionTarget } from './CollectionDialog'
import { DeleteCollectionDialog } from './DeleteCollectionDialog'
import { buildTree, type CollectionNode, type TreeNode } from './tree'
import { useSitesData } from './use-sites-data'

/** Indentation of each nesting level, in pixels. */
const indentPerLevel = 14

/** What can be done to a collection from its row. */
interface CollectionActions {
  /** Called when a collection is opened or closed. */
  onToggle: (id: string) => void
  /** Called to create a collection inside another. */
  onCreateIn: (parentId: string) => void
  /** Called to rename or move a collection. */
  onEdit: (id: string) => void
  /** Called to delete a collection. */
  onDelete: (id: string) => void
}

/** Props of {@link Row}. */
interface RowProps {
  /** The row to draw. */
  node: TreeNode
  /** What the row's collection can do. */
  actions: CollectionActions
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
  const style = { paddingInlineStart: `${node.depth * indentPerLevel + 8}px` }
  const base = 'flex h-[30px] w-full items-center gap-2 rounded-md pe-2 text-start text-[13px] hover:bg-muted'

  if (node.kind === 'collection') {
    return (
      <div
        role="treeitem"
        aria-expanded={node.expanded}
        aria-level={node.depth + 1}
        tabIndex={0}
        onClick={() => actions.onToggle(node.id)}
        onKeyDown={(event) => onCollectionKey(event, node, actions.onToggle)}
        className={cn(base, 'group cursor-pointer font-medium')}
        style={style}
      >
        <ChevronRight className={cn('size-3 shrink-0 text-faint transition-transform rtl:-scale-x-100', node.expanded && 'rotate-90 rtl:rotate-90')} />
        <Folder className="size-[15px] shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate">{node.name}</span>
        <span className="font-mono text-[11px] text-muted-foreground group-hover:hidden group-focus-within:hidden">{node.siteCount}</span>
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label={sites(($) => $.collection.actions, { name: node.name })}
            onClick={(event) => event.stopPropagation()}
            className="hidden size-5 items-center justify-center rounded text-muted-foreground hover:bg-background group-focus-within:flex group-hover:flex"
          >
            <Ellipsis className="size-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
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
      <div role="treeitem" aria-level={1} tabIndex={0} className={cn(base, 'italic')} style={style}>
        <span className="w-3 shrink-0" />
        <Zap className="size-[15px] shrink-0 text-warn" />
        <span className="min-w-0 flex-1 truncate">{node.name}</span>
        <span className="text-[11px] text-muted-foreground">{t(($) => $.sidebar.notSaved)}</span>
      </div>
    )
  }
  return (
    <div role="treeitem" aria-level={node.depth + 1} tabIndex={0} className={base} style={style}>
      <span className="w-3 shrink-0" />
      <span className="mx-[3px] size-2 shrink-0 rounded-full bg-muted-foreground/50" />
      <span className="min-w-0 flex-1 truncate">{node.name}</span>
      <span className="max-w-24 truncate font-mono text-[11px] text-muted-foreground">{node.host}</span>
    </div>
  )
}

/** Returns the open collections after opening or closing one. */
function toggled(open: ReadonlySet<string>, id: string): ReadonlySet<string> {
  return new Set(open.has(id) ? [...open].filter((item) => item !== id) : [...open, id])
}

/** Sidebar of the shell: quick connect and the tree of collections and sites. */
export function SitesSidebar() {
  const { t } = useTranslation('shell')
  const workspace = useWorkspace()
  const setQuickConnectOpen = useDialogsStore((state) => state.setQuickConnectOpen)
  const { collections, sites, quick } = useSitesData(workspace?.id)
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [target, setTarget] = useState<CollectionTarget | null>(null)
  const [deleting, setDeleting] = useState<Collection | null>(null)
  const rows = useMemo(() => buildTree({ collections, sites, quick, expanded }), [collections, sites, quick, expanded])
  const actions: CollectionActions = {
    onToggle: (id) => setExpanded((open) => toggled(open, id)),
    onCreateIn: (parentId) => {
      setExpanded((open) => new Set([...open, parentId]))
      setTarget({ mode: 'create', parentId })
    },
    onEdit: (id) => setTarget({ mode: 'edit', collection: collections.find((collection) => collection.id === id) as Collection }),
    onDelete: (id) => setDeleting(collections.find((collection) => collection.id === id) as Collection),
  }

  return (
    <nav aria-label={t(($) => $.sidebar.label)} className="flex w-62 flex-col border-e bg-card">
      <div className="flex items-center justify-between px-4 pt-3.5 pb-2">
        <h2 className="text-[11px] font-medium tracking-widest text-muted-foreground uppercase">{t(($) => $.sidebar.sites)}</h2>
        <div className="flex gap-1">
          <Button variant="outline" size="icon-xs" aria-label={t(($) => $.sidebar.newCollection)} onClick={() => setTarget({ mode: 'create' })}>
            <FolderPlus />
          </Button>
          <Button variant="outline" size="icon-xs" aria-label={t(($) => $.sidebar.newSite)}>
            <Plus />
          </Button>
        </div>
      </div>
      <div className="px-2 pb-2.5">
        <Button variant="outline" className="h-8 w-full justify-start gap-2 bg-muted/40" onClick={() => setQuickConnectOpen(true)}>
          <Zap className="text-warn" />
          <span className="flex-1 text-start">{t(($) => $.sidebar.quickConnect)}</span>
          <kbd className="rounded-md border px-1.5 font-mono text-[11px] text-muted-foreground">⌘⇧K</kbd>
        </Button>
      </div>
      <div role="tree" aria-label={t(($) => $.sidebar.sites)} className="flex flex-col gap-px overflow-y-auto px-2">
        {rows.map((node) => (
          <Row key={`${node.kind}-${node.id}`} node={node} actions={actions} />
        ))}
      </div>
      {rows.length === 0 && <p className="px-4 text-sm text-muted-foreground">{t(($) => $.sidebar.empty)}</p>}
      {workspace !== undefined && (
        <>
          <CollectionDialog target={target} workspaceId={workspace.id} collections={collections} onClose={() => setTarget(null)} />
          <DeleteCollectionDialog collection={deleting} workspaceId={workspace.id} onClose={() => setDeleting(null)} />
        </>
      )}
    </nav>
  )
}
