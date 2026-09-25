import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { Collection } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { sitesKeys } from './use-sites-data'

/** Props of {@link DeleteCollectionDialog}. */
export interface DeleteCollectionDialogProps {
  /** The collection to delete; the dialog is closed when there is none. */
  collection: Collection | null
  /** Workspace the collection belongs to. */
  workspaceId: string
  /** Called when the dialog is dismissed or the collection is gone. */
  onClose: () => void
}

/** Props of {@link DeleteBody}. */
interface DeleteBodyProps extends Omit<DeleteCollectionDialogProps, 'collection'> {
  /** The collection to delete. */
  collection: Collection
}

/** Confirmation text and buttons of the delete dialog. */
function DeleteBody({ collection, workspaceId, onClose }: DeleteBodyProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(($) => $.collection.deleteTitle)}</DialogTitle>
        <DialogDescription>{t(($) => $.collection.deleteBody, { name: collection.name })}</DialogDescription>
      </DialogHeader>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t(($) => $.collection.cancel)}
        </Button>
        <Button
          variant="destructive"
          onClick={async () => {
            try {
              await api.collections.remove(workspaceId, collection.id)
              await queryClient.invalidateQueries({ queryKey: sitesKeys.collections(workspaceId) })
              onClose()
            } catch (error) {
              setFailure(siteErrorKeyOf(error))
            }
          }}
        >
          {t(($) => $.collection.deleteConfirm)}
        </Button>
      </DialogFooter>
    </>
  )
}

/** Asks for confirmation before deleting an empty collection. */
export function DeleteCollectionDialog({ collection, ...rest }: DeleteCollectionDialogProps) {
  return (
    <Dialog open={collection !== null} onOpenChange={rest.onClose}>
      <DialogContent className="sm:max-w-[420px]">{collection !== null && <DeleteBody collection={collection} {...rest} />}</DialogContent>
    </Dialog>
  )
}
