import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { SessionInfo } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { securityErrorKeyOf, type SecurityErrorKey } from './errors'

/** Props of {@link MfaStep}. */
export interface MfaStepProps {
  /** The challenge of the password step. */
  challenge: string
  /** Called with the session once the code was right. */
  onVerified: (session: SessionInfo) => void | Promise<void>
  /** Called to go back to the password step, also when the challenge expired. */
  onRestart: (expired: boolean) => void
}

/** The second step of signing in: a code from the authenticator app or a recovery code. */
export function MfaStep({ challenge, onVerified, onRestart }: MfaStepProps) {
  const { t } = useTranslation('security')
  const api = useApi()
  const [code, setCode] = useState('')
  const [failure, setFailure] = useState<SecurityErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (code.trim() === '') {
      setFailure('codeRequired')
      return
    }
    setBusy(true)
    try {
      await onVerified(await api.auth.verifyMfa(challenge, code.trim()))
    } catch (error) {
      const key = securityErrorKeyOf(error)
      if (key === 'invalid_challenge') {
        onRestart(true)
        return
      }
      setFailure(key)
      setCode('')
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">{t(($) => $.login.subtitle)}</p>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="mfa-code">{t(($) => $.login.code)}</Label>
        <Input id="mfa-code" autoComplete="one-time-code" autoFocus spellCheck={false} className="font-mono tracking-widest" value={code} onChange={(event) => setCode(event.target.value)} />
        <p className="text-xs text-muted-foreground">{t(($) => $.login.hint)}</p>
      </div>
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <Button type="submit" size="lg" disabled={busy}>
        {t(($) => $.login.submit)}
      </Button>
      <Button type="button" variant="ghost" onClick={() => onRestart(false)}>
        {t(($) => $.login.back)}
      </Button>
    </form>
  )
}
