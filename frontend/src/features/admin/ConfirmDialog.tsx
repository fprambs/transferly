import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

import { AdminFailure } from './AdminMessage'
import { adminErrorKeyOf, type AdminErrorKey } from './errors'

/** Props of {@link ConfirmDialog}. */
export interface ConfirmDialogProps {
  /** Whether the dialog is open. */
  open: boolean
  /** Question the dialog asks. */
  title: string
  /** What will happen if the administrator confirms. */
  body: string
  /** Text of the button that confirms. */
  confirm: string
  /** Does the change; the dialog closes when it succeeds and explains why when it fails. */
  onConfirm: () => Promise<void>
  /** Called when the dialog is dismissed or the change is done. */
  onClose: () => void
}

/** Title, text and buttons of the dialog. It is mounted only while the dialog is open, so a failure does not outlive it. */
function ConfirmBody({ title, body, confirm, onConfirm, onClose }: Omit<ConfirmDialogProps, 'open'>) {
  const { t } = useTranslation('admin')
  const [failure, setFailure] = useState<AdminErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  return (
    <div className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{body}</DialogDescription>
      </DialogHeader>
      <AdminFailure failure={failure} />
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t(($) => $.confirm.cancel)}
        </Button>
        <Button
          variant="destructive"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            try {
              await onConfirm()
              onClose()
            } catch (error) {
              setFailure(adminErrorKeyOf(error))
              setBusy(false)
            }
          }}
        >
          {confirm}
        </Button>
      </DialogFooter>
    </div>
  )
}

/** A confirmation before a change that cannot be undone from this screen, with a destructive button. */
export function ConfirmDialog({ open, onClose, ...body }: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent>{open && <ConfirmBody {...body} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
