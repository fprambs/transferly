import { useQueryClient } from '@tanstack/react-query'
import { Bookmark as BookmarkIcon, BookmarkPlus, Folder, Settings2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { buttonVariants } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { Bookmark } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'

import { canManageSites } from '../teams/roles'
import { useWorkspace } from '../workspace/use-workspace'

import { BookmarkDialog } from './BookmarkDialog'
import { ManageBookmarksDialog } from './ManageBookmarksDialog'
import { bookmarksKey, useBookmarks } from './use-bookmarks'

/** The dialog open over the menu. */
type Open = { kind: 'add' } | { kind: 'manage' } | { kind: 'edit'; bookmark: Bookmark }

/** Props of {@link BookmarksMenu}. */
export interface BookmarksMenuProps {
  /** Workspace of the site. */
  workspaceId: string
  /** Saved site whose bookmarks are offered. */
  siteId: string
  /** Name of the site, for the dialogs. */
  siteName: string
  /** Folder in view, which can be bookmarked. */
  path: string
  /** Called with the folder of a chosen bookmark. */
  onNavigate: (path: string) => void
}

/** The name a bookmark of a folder starts with: the folder's own name, or the site's for the root. */
function suggestedName(path: string, siteName: string): string {
  return path.split('/').filter(Boolean).at(-1) ?? siteName
}

/**
 * The bookmarks of a saved site in the explorer toolbar, as FileZilla's site bookmarks (4.14b): its icon is filled while the folder in view
 * is bookmarked, and its menu opens a bookmark, bookmarks the folder in view and manages the bookmarks. Whoever may only browse the
 * workspace sees and opens them; changing them needs the right to manage sites.
 */
export function BookmarksMenu({ workspaceId, siteId, siteName, path, onNavigate }: BookmarksMenuProps) {
  const { t } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const workspace = useWorkspace()
  const bookmarks = useBookmarks(workspaceId, siteId)
  const [open, setOpen] = useState<Open | null>(null)
  const list = bookmarks.data ?? []
  const marked = list.some((bookmark) => bookmark.path === path)
  const editable = workspace !== undefined && canManageSites(workspace.role)
  const refresh = (): Promise<void> => queryClient.invalidateQueries({ queryKey: bookmarksKey(workspaceId, siteId) })

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger aria-label={t(($) => $.bookmarks.title)} className={cn(buttonVariants({ variant: 'ghost', size: 'icon-sm' }), 'shrink-0')}>
          <BookmarkIcon aria-hidden="true" className={cn(marked && 'fill-warn text-warn')} />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-72">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.bookmarks.title)}</DropdownMenuLabel>
            {list.length === 0 ? <p className="px-1.5 py-1 text-sm text-muted-foreground">{t(($) => $.bookmarks.empty)}</p> : null}
            {list.map((bookmark) => (
              <DropdownMenuItem key={bookmark.id} onClick={() => onNavigate(bookmark.path)}>
                <Folder className="text-accent-text" />
                <span className="min-w-0 flex-1 truncate">{bookmark.name}</span>
                <span className="max-w-32 truncate font-mono text-xs text-muted-foreground">{bookmark.path}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          {editable ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem disabled={marked} onClick={() => setOpen({ kind: 'add' })}>
                <BookmarkPlus />
                {t(($) => $.bookmarks.add.menu)}
              </DropdownMenuItem>
              <DropdownMenuItem disabled={list.length === 0} onClick={() => setOpen({ kind: 'manage' })}>
                <Settings2 />
                {t(($) => $.bookmarks.manage.menu)}
              </DropdownMenuItem>
            </>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>
      {open?.kind === 'add' && (
        <BookmarkDialog
          mode="add"
          siteName={siteName}
          initial={{ name: suggestedName(path, siteName), path }}
          onSubmit={async (bookmark) => {
            await api.bookmarks.create(workspaceId, siteId, bookmark)
            await refresh()
          }}
          onClose={() => setOpen(null)}
        />
      )}
      {open?.kind === 'manage' && (
        <ManageBookmarksDialog
          workspaceId={workspaceId}
          siteId={siteId}
          siteName={siteName}
          bookmarks={list}
          onEdit={(bookmark) => setOpen({ kind: 'edit', bookmark })}
          onClose={() => setOpen(null)}
        />
      )}
      {open?.kind === 'edit' && (
        <BookmarkDialog
          mode="edit"
          siteName={siteName}
          initial={{ name: open.bookmark.name, path: open.bookmark.path }}
          onSubmit={async (bookmark) => {
            await api.bookmarks.update(workspaceId, siteId, open.bookmark.id, bookmark)
            await refresh()
          }}
          onClose={() => setOpen({ kind: 'manage' })}
        />
      )}
    </>
  )
}
