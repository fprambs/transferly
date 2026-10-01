import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useApi } from '@/lib/api/use-api'

import { securityErrorKeyOf, type SecurityErrorKey } from './errors'
import { RecoveryCodes } from './RecoveryCodes'

/** The sensitive changes that need the password again. */
export type PasswordAction = 'disable' | 'regenerate'

/** Props of {@link PasswordDialog}. */
export interface PasswordDialogProps {
  /** What to confirm; the dialog is closed when there is none. */
  action: PasswordAction | null
  /** Called when the dialog is dismissed or the change is done. */
  onClose: () => void
}

/** Form of the dialog and, after renewing the recovery codes, the new codes. */
function PasswordBody({ action, onClose }: PasswordDialogProps & { action: PasswordAction }) {
  const { t } = useTranslation('security')
  const api = useApi()
  const [password, setPassword] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const [failure, setFailure] = useState<SecurityErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  if (codes !== null) {
    return <RecoveryCodes codes={codes} onDone={onClose} />
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (password === '') {
      setFailure('passwordRequired')
      return
    }
    setBusy(true)
    try {
      if (action === 'disable') {
        await api.auth.disableTotp(password)
        onClose()
        return
      }
      setCodes((await api.auth.regenerateRecoveryCodes(password)).codes)
    } catch (error) {
      setFailure(securityErrorKeyOf(error))
      setPassword('')
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{action === 'disable' ? t(($) => $.password.disableTitle) : t(($) => $.password.regenerateTitle)}</DialogTitle>
        <DialogDescription>{action === 'disable' ? t(($) => $.password.disableBody) : t(($) => $.password.regenerateBody)}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="security-password">{t(($) => $.password.password)}</Label>
        <Input id="security-password" type="password" autoComplete="current-password" autoFocus value={password} onChange={(event) => setPassword(event.target.value)} />
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.password.cancel)}
        </Button>
        <Button type="submit" variant={action === 'disable' ? 'destructive' : 'default'} disabled={busy}>
          {action === 'disable' ? t(($) => $.password.confirmDisable) : t(($) => $.password.confirmRegenerate)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Asks for the account password before turning the second factor off or replacing the recovery codes. */
export function PasswordDialog({ action, onClose }: PasswordDialogProps) {
  return (
    <Dialog open={action !== null} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[420px]">{action !== null && <PasswordBody action={action} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
