import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { Passkey } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { securityErrorKeyOf, type SecurityErrorKey } from './errors'

/** Props of {@link RemovePasskeyDialog}. */
export interface RemovePasskeyDialogProps {
  /** The passkey to remove; the dialog is closed when there is none. */
  passkey: Passkey | null
  /** Called when the dialog is dismissed or the passkey is gone. */
  onClose: () => void
}

/** Password form of the dialog. */
function RemoveBody({ passkey, onClose }: RemovePasskeyDialogProps & { passkey: Passkey }) {
  const { t } = useTranslation('security')
  const api = useApi()
  const [password, setPassword] = useState('')
  const [failure, setFailure] = useState<SecurityErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (password === '') {
      setFailure('passwordRequired')
      return
    }
    setBusy(true)
    try {
      await api.auth.removePasskey(passkey.id, password)
      onClose()
    } catch (error) {
      setFailure(securityErrorKeyOf(error))
      setPassword('')
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.removePasskey.title, { name: passkey.name })}</DialogTitle>
        <DialogDescription>{t(($) => $.removePasskey.body)}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="remove-passkey-password">{t(($) => $.password.password)}</Label>
        <Input id="remove-passkey-password" type="password" autoComplete="current-password" autoFocus value={password} onChange={(event) => setPassword(event.target.value)} />
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.password.cancel)}
        </Button>
        <Button type="submit" variant="destructive" disabled={busy}>
          {t(($) => $.removePasskey.confirm)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Asks for the account password before removing a passkey. */
export function RemovePasskeyDialog({ passkey, onClose }: RemovePasskeyDialogProps) {
  return (
    <Dialog open={passkey !== null} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[420px]">{passkey !== null && <RemoveBody passkey={passkey} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
