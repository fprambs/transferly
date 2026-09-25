import { ChevronRight, Folder, FolderPlus, Plus, Zap } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useDialogsStore } from '@/state/dialogs-store'

import { useWorkspace } from '../workspace/use-workspace'

import { buildTree, type TreeNode } from './tree'
import { useSitesData } from './use-sites-data'

/** Indentation of each nesting level, in pixels. */
const indentPerLevel = 14

/** Props of {@link Row}. */
interface RowProps {
  /** The row to draw. */
  node: TreeNode
  /** Called when a collection is opened or closed. */
  onToggle: (id: string) => void
}

/** One row of the sites tree: a collection, a saved site or a quick connection. */
function Row({ node, onToggle }: RowProps) {
  const { t } = useTranslation('shell')
  const style = { paddingInlineStart: `${node.depth * indentPerLevel + 8}px` }
  const base = 'flex h-[30px] w-full items-center gap-2 rounded-md pe-2 text-start text-[13px] hover:bg-muted'

  if (node.kind === 'collection') {
    return (
      <button
        type="button"
        role="treeitem"
        aria-expanded={node.expanded}
        aria-level={node.depth + 1}
        onClick={() => onToggle(node.id)}
        className={cn(base, 'font-medium')}
        style={style}
      >
        <ChevronRight className={cn('size-3 shrink-0 text-faint transition-transform rtl:-scale-x-100', node.expanded && 'rotate-90 rtl:rotate-90')} />
        <Folder className="size-[15px] shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate">{node.name}</span>
        <span className="font-mono text-[11px] text-muted-foreground">{node.siteCount}</span>
      </button>
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
  const rows = useMemo(() => buildTree({ collections, sites, quick, expanded }), [collections, sites, quick, expanded])

  return (
    <nav aria-label={t(($) => $.sidebar.label)} className="flex w-62 flex-col border-e bg-card">
      <div className="flex items-center justify-between px-4 pt-3.5 pb-2">
        <h2 className="text-[11px] font-medium tracking-widest text-muted-foreground uppercase">{t(($) => $.sidebar.sites)}</h2>
        <div className="flex gap-1">
          <Button variant="outline" size="icon-xs" aria-label={t(($) => $.sidebar.newCollection)}>
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
          <Row key={`${node.kind}-${node.id}`} node={node} onToggle={(id) => setExpanded((open) => toggled(open, id))} />
        ))}
      </div>
      {rows.length === 0 && <p className="px-4 text-sm text-muted-foreground">{t(($) => $.sidebar.empty)}</p>}
    </nav>
  )
}
