import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

import type { UploadConflict } from './use-uploads'

/** Props of {@link UploadConflictDialog}. */
export interface UploadConflictDialogProps {
  /** The file that already exists on the server. */
  conflict: UploadConflict
}

/** Asks whether a file that already exists on the server should be overwritten or skipped. */
export function UploadConflictDialog({ conflict }: UploadConflictDialogProps) {
  const { t } = useTranslation('explorer')

  return (
    <Dialog open onOpenChange={() => conflict.decide(false)}>
      <DialogContent className="sm:max-w-[440px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.upload.conflictTitle, { name: conflict.file.name })}</DialogTitle>
          <DialogDescription>{t(($) => $.upload.conflictBody)}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => conflict.decide(false)}>
            {t(($) => $.upload.skip)}
          </Button>
          <Button variant="destructive" onClick={() => conflict.decide(true)}>
            {t(($) => $.upload.overwrite)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
