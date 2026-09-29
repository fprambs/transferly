import { ChevronsRight, Folder } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { buttonVariants } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { Bookmark } from '@/lib/api'
import { cn } from '@/lib/utils'

import { fitting } from './fitting'
import { useBookmarks } from './use-bookmarks'

/** Classes of a chip, shared by the chips shown and the ones measured. */
function chipClass(active: boolean): string {
  return cn(
    'flex h-6 max-w-44 shrink-0 items-center gap-1.5 rounded-full border px-2.5 text-xs whitespace-nowrap transition-colors',
    active ? 'border-primary/40 bg-primary/15 font-medium text-foreground' : 'border-border bg-muted/40 text-muted-foreground hover:bg-accent hover:text-foreground',
  )
}

/** The inside of a chip: a folder and the name of the bookmark. */
function ChipContent({ bookmark }: { bookmark: Bookmark }) {
  return (
    <>
      <Folder aria-hidden="true" className="size-3.5 shrink-0 text-accent-text" />
      <span className="truncate">{bookmark.name}</span>
    </>
  )
}

/** Props of {@link BookmarksBar}. */
export interface BookmarksBarProps {
  /** Workspace of the site. */
  workspaceId: string
  /** Saved site whose bookmarks are shown. */
  siteId: string
  /** Folder in view, whose chip is marked. */
  path: string
  /** Called with the folder of a chosen bookmark. */
  onNavigate: (path: string) => void
}

/**
 * The bookmarks of a saved site as chips in the explorer toolbar, against the bookmarks menu in the room the path leaves, as Chrome's
 * bookmarks bar (4.14g): the chips that fit are shown in order, the one of the folder in view marked, and a button holds the rest. It is
 * left out while the site has no bookmarks.
 */
export function BookmarksBar({ workspaceId, siteId, path, onNavigate }: BookmarksBarProps) {
  const { t } = useTranslation('explorer')
  const bookmarks = useBookmarks(workspaceId, siteId)
  const list = bookmarks.data ?? []
  const bar = useRef<HTMLDivElement>(null)
  const ruler = useRef<HTMLDivElement>(null)
  const [shown, setShown] = useState(Number.POSITIVE_INFINITY)

  useLayoutEffect(() => {
    const measured = bar.current
    if (measured === null) {
      return
    }
    const update = (): void => {
      const widths = Array.from((ruler.current as HTMLDivElement).children, (chip) => (chip as HTMLElement).offsetWidth)
      setShown(measured.clientWidth === 0 ? widths.length : fitting(widths, measured.clientWidth))
    }
    update()
    const observer = new ResizeObserver(update)
    observer.observe(measured)
    return () => observer.disconnect()
  }, [bookmarks.data])

  if (list.length === 0) {
    return null
  }
  const visible = list.slice(0, shown)
  const hidden = list.slice(shown)
  return (
    <nav aria-label={t(($) => $.bookmarks.bar)} className="relative flex min-w-0 flex-1 items-center justify-end ps-2">
      <div ref={ruler} aria-hidden="true" className="pointer-events-none invisible absolute flex gap-1">
        {list.map((bookmark) => (
          <span key={bookmark.id} className={chipClass(false)}>
            <ChipContent bookmark={bookmark} />
          </span>
        ))}
      </div>
      <div ref={bar} className="flex min-w-0 flex-1 items-center justify-end gap-1 overflow-hidden">
        {visible.map((bookmark) => (
          <button key={bookmark.id} type="button" title={bookmark.path} aria-current={bookmark.path === path ? 'location' : undefined} onClick={() => onNavigate(bookmark.path)} className={chipClass(bookmark.path === path)}>
            <ChipContent bookmark={bookmark} />
          </button>
        ))}
        {hidden.length > 0 && (
          <DropdownMenu>
            <DropdownMenuTrigger aria-label={t(($) => $.bookmarks.more, { count: hidden.length })} className={cn(buttonVariants({ variant: 'ghost', size: 'icon-xs' }), 'shrink-0')}>
              <ChevronsRight aria-hidden="true" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              {hidden.map((bookmark) => (
                <DropdownMenuItem key={bookmark.id} onClick={() => onNavigate(bookmark.path)}>
                  <Folder className="text-accent-text" />
                  <span className="min-w-0 flex-1 truncate">{bookmark.name}</span>
                  <span className="max-w-28 truncate font-mono text-xs text-muted-foreground">{bookmark.path}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </nav>
  )
}
