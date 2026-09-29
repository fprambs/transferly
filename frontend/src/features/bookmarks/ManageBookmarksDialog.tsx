import { useQueryClient } from '@tanstack/react-query'
import { Pencil, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { Bookmark } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { bookmarkErrorKeyOf, type BookmarkErrorKey } from './errors'
import { bookmarksKey } from './use-bookmarks'

/** Props of {@link ManageBookmarksDialog}. */
export interface ManageBookmarksDialogProps {
  /** Workspace of the site. */
  workspaceId: string
  /** Saved site the bookmarks belong to. */
  siteId: string
  /** Name of the site, for the description. */
  siteName: string
  /** The bookmarks of the site. */
  bookmarks: Bookmark[]
  /** Called to change a bookmark. */
  onEdit: (bookmark: Bookmark) => void
  /** Called when the dialog is dismissed. */
  onClose: () => void
}

/** Lists the bookmarks of a site to rename, move or remove each one (4.14b). */
export function ManageBookmarksDialog({ workspaceId, siteId, siteName, bookmarks, onEdit, onClose }: ManageBookmarksDialogProps) {
  const { t } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<BookmarkErrorKey | null>(null)

  const remove = async (bookmark: Bookmark): Promise<void> => {
    setFailure(null)
    try {
      await api.bookmarks.remove(workspaceId, siteId, bookmark.id)
    } catch (error) {
      setFailure(bookmarkErrorKeyOf(error))
    }
    await queryClient.invalidateQueries({ queryKey: bookmarksKey(workspaceId, siteId) })
  }

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.bookmarks.manage.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.bookmarks.manage.description, { site: siteName })}</DialogDescription>
        </DialogHeader>
        {bookmarks.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t(($) => $.bookmarks.empty)}</p>
        ) : (
          <ul aria-label={t(($) => $.bookmarks.title)} className="flex max-h-80 flex-col divide-y overflow-y-auto rounded-lg border">
            {bookmarks.map((bookmark) => (
              <li key={bookmark.id} className="flex items-center gap-2 px-3 py-2">
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate text-sm font-medium">{bookmark.name}</span>
                  <span className="truncate font-mono text-xs text-muted-foreground">{bookmark.path}</span>
                </div>
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.bookmarks.manage.edit, { name: bookmark.name })} onClick={() => onEdit(bookmark)}>
                  <Pencil />
                </Button>
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.bookmarks.manage.remove, { name: bookmark.name })} onClick={() => void remove(bookmark)}>
                  <Trash2 className="text-destructive" />
                </Button>
              </li>
            ))}
          </ul>
        )}
        {failure !== null && (
          <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {t(($) => $.bookmarks.errors[failure])}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.bookmarks.manage.close)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
