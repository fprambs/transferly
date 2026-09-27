import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useApi } from '@/lib/api/use-api'
import { createPasskey, passkeysSupported } from '@/lib/webauthn'

import { securityErrorKeyOf, type SecurityErrorKey } from './errors'
import { RecoveryCodes } from './RecoveryCodes'

/** Props of {@link AddPasskeyDialog}. */
export interface AddPasskeyDialogProps {
  /** Whether the dialog is open. */
  open: boolean
  /** Called when the dialog is dismissed or the passkey was added. */
  onClose: () => void
}

/** Name form of the dialog and, when the account had no recovery codes, the codes the first passkey brings. It is mounted only while the dialog is open. */
function AddBody({ onClose }: Pick<AddPasskeyDialogProps, 'onClose'>) {
  const { t } = useTranslation('security')
  const api = useApi()
  const [name, setName] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const [failure, setFailure] = useState<SecurityErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  if (codes !== null) {
    return <RecoveryCodes codes={codes} onDone={onClose} />
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!passkeysSupported()) {
      setFailure('passkeyUnsupported')
      return
    }
    setBusy(true)
    setFailure(null)
    try {
      const { ceremony, options } = await api.auth.beginPasskey()
      const credential = await createPasskey(options)
      const added = await api.auth.finishPasskey(ceremony, name.trim(), credential)
      if (added.recoveryCodes === undefined) {
        onClose()
        return
      }
      setCodes(added.recoveryCodes)
    } catch (error) {
      setFailure(error instanceof DOMException ? 'passkeyCancelled' : securityErrorKeyOf(error))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.addPasskey.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.addPasskey.body)}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="passkey-name">{t(($) => $.addPasskey.name)}</Label>
        <Input id="passkey-name" autoFocus autoComplete="off" maxLength={60} placeholder={t(($) => $.addPasskey.placeholder)} value={name} onChange={(event) => setName(event.target.value)} />
      </div>
      {busy && (
        <p role="status" className="text-sm text-muted-foreground">
          {t(($) => $.addPasskey.working)}
        </p>
      )}
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.addPasskey.cancel)}
        </Button>
        <Button type="submit" disabled={busy}>
          {t(($) => $.addPasskey.confirm)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Adds a passkey: asks for a name, has the browser make the credential and stores it. */
export function AddPasskeyDialog({ open, onClose }: AddPasskeyDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[440px]">{open && <AddBody onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
