import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { RemoteEntry } from '@/lib/api'

import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'

/** Props of {@link DeleteEntryDialog}. */
export interface DeleteEntryDialogProps {
  /** The entry to delete. */
  entry: RemoteEntry
  /** Performs the deletion; for a folder, `recursive` says whether its contents go too. A failure is shown in the dialog. */
  onDelete: (recursive: boolean) => Promise<void>
  /** Called when the dialog is dismissed or the entry is gone. */
  onClose: () => void
}

/** Asks for confirmation before deleting a file or a folder, and whether a folder's contents go with it. */
export function DeleteEntryDialog({ entry, onDelete, onClose }: DeleteEntryDialogProps) {
  const { t } = useTranslation('explorer')
  const [recursive, setRecursive] = useState(false)
  const [failure, setFailure] = useState<ExplorerErrorKey | null>(null)
  const folder = entry.type === 'directory'

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{folder ? t(($) => $.delete.titleFolder) : t(($) => $.delete.titleFile)}</DialogTitle>
          <DialogDescription>{t(($) => $.delete.body, { name: entry.name })}</DialogDescription>
        </DialogHeader>
        {folder && (
          <div className="flex items-center gap-2 text-sm">
            <Checkbox id="delete-recursive" aria-labelledby="delete-recursive-label" checked={recursive} onCheckedChange={setRecursive} />
            <span id="delete-recursive-label">{t(($) => $.delete.recursive)}</span>
          </div>
        )}
        {failure !== null && (
          <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {t(($) => $.errors[failure])}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.cancel)}
          </Button>
          <Button
            variant="destructive"
            onClick={async () => {
              try {
                await onDelete(recursive)
                onClose()
              } catch (error) {
                setFailure(explorerErrorKeyOf(error))
              }
            }}
          >
            {t(($) => $.delete.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
