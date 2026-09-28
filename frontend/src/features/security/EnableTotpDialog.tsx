import { useQuery } from '@tanstack/react-query'
import QRCode from 'qrcode'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Loading } from '@/components/ui/loading'
import { useApi } from '@/lib/api/use-api'

import { securityErrorKeyOf, type SecurityErrorKey } from './errors'
import { RecoveryCodes } from './RecoveryCodes'

/** Props of {@link EnableTotpDialog}. */
export interface EnableTotpDialogProps {
  /** Whether the dialog is open. */
  open: boolean
  /** Called when the dialog is dismissed or the setup is done. */
  onClose: () => void
}

/** Setup and confirmation of the app, and then the recovery codes. It is mounted only while the dialog is open, so every opening starts a new secret. */
function EnableBody({ onClose }: Pick<EnableTotpDialogProps, 'onClose'>) {
  const { t } = useTranslation('security')
  const api = useApi()
  const setup = useQuery({ queryKey: ['totp-setup'], queryFn: () => api.auth.beginTotp(), staleTime: Infinity, gcTime: 0 })
  const qr = useQuery({
    queryKey: ['totp-qr', setup.data?.uri],
    queryFn: () => QRCode.toDataURL(setup.data?.uri as string, { errorCorrectionLevel: 'M', margin: 1, width: 192 }),
    enabled: setup.data !== undefined,
  })
  const [code, setCode] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const [failure, setFailure] = useState<SecurityErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  if (codes !== null) {
    return <RecoveryCodes codes={codes} onDone={onClose} />
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (code.trim() === '') {
      setFailure('codeRequired')
      return
    }
    setBusy(true)
    try {
      setCodes((await api.auth.confirmTotp(code.trim())).codes)
    } catch (error) {
      setFailure(securityErrorKeyOf(error))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.enable.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.enable.scan)}</DialogDescription>
      </DialogHeader>
      {setup.isPending ? (
        <Loading label={t(($) => $.enable.working)} />
      ) : (
        <div className="flex flex-col items-center gap-3">
          {qr.data !== undefined && <img src={qr.data} alt={t(($) => $.enable.qrAlt)} width={192} height={192} className="rounded-lg bg-white p-1" />}
          <div className="flex w-full flex-col gap-1 text-center text-xs text-muted-foreground">
            <span>{t(($) => $.enable.manual)}</span>
            <code className="font-mono text-sm break-all text-foreground">{setup.data?.secret}</code>
          </div>
        </div>
      )}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="totp-code">{t(($) => $.enable.code)}</Label>
        <Input id="totp-code" inputMode="numeric" autoComplete="one-time-code" className="font-mono tracking-widest" value={code} onChange={(event) => setCode(event.target.value)} />
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.enable.cancel)}
        </Button>
        <Button type="submit" disabled={busy || setup.isPending}>
          {t(($) => $.enable.confirm)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Turns the authenticator app on: shows the secret to scan, asks for a code that proves it worked and then shows the recovery codes. */
export function EnableTotpDialog({ open, onClose }: EnableTotpDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[460px]">{open && <EnableBody onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
