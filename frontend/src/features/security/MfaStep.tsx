import { KeyRound } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { MfaChallenge, SessionInfo } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { getPasskey, passkeysSupported } from '@/lib/webauthn'

import { securityErrorKeyOf, type SecurityErrorKey } from './errors'

/** Props of {@link MfaStep}. */
export interface MfaStepProps {
  /** The answer of the password step: the challenge and the ways to answer it. */
  challenge: MfaChallenge
  /** Called with the session once the second factor was right. */
  onVerified: (session: SessionInfo) => void | Promise<void>
  /** Called to go back to the password step, also when the challenge expired. */
  onRestart: (expired: boolean) => void
}

/** The second step of signing in: a passkey, a code from the authenticator app or a recovery code. */
export function MfaStep({ challenge, onVerified, onRestart }: MfaStepProps) {
  const { t } = useTranslation('security')
  const api = useApi()
  const [code, setCode] = useState('')
  const [failure, setFailure] = useState<SecurityErrorKey | null>(null)
  const [busy, setBusy] = useState(false)
  const withPasskey = challenge.methods.includes('passkey')
  const withCode = challenge.methods.includes('totp') || challenge.methods.includes('recovery')

  const fail = (key: SecurityErrorKey) => {
    if (key === 'invalid_challenge') {
      onRestart(true)
      return
    }
    setFailure(key)
    setBusy(false)
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (code.trim() === '') {
      setFailure('codeRequired')
      return
    }
    setBusy(true)
    try {
      await onVerified(await api.auth.verifyMfa(challenge.challenge, code.trim()))
    } catch (error) {
      setCode('')
      fail(securityErrorKeyOf(error))
    }
  }

  const signInWithPasskey = async () => {
    if (!passkeysSupported()) {
      setFailure('passkeyUnsupported')
      return
    }
    setBusy(true)
    setFailure(null)
    try {
      const { ceremony, options } = await api.auth.beginPasskeyLogin(challenge.challenge)
      const credential = await getPasskey(options)
      await onVerified(await api.auth.finishPasskeyLogin(challenge.challenge, ceremony, credential))
    } catch (error) {
      fail(error instanceof DOMException ? 'passkeyCancelled' : securityErrorKeyOf(error))
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {withPasskey && (
        <div className="flex flex-col gap-2">
          <Button type="button" size="lg" disabled={busy} onClick={() => void signInWithPasskey()}>
            <KeyRound className="size-4" />
            {t(($) => $.login.passkey)}
          </Button>
          <p className="text-xs text-muted-foreground">{t(($) => $.login.passkeyHint)}</p>
        </div>
      )}
      {withPasskey && withCode && <p className="text-center text-xs text-muted-foreground uppercase">{t(($) => $.login.or)}</p>}
      {withCode && (
        <form onSubmit={submit} noValidate className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">{t(($) => $.login.subtitle)}</p>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="mfa-code">{t(($) => $.login.code)}</Label>
            <Input id="mfa-code" autoComplete="one-time-code" autoFocus={!withPasskey} spellCheck={false} className="font-mono tracking-widest" value={code} onChange={(event) => setCode(event.target.value)} />
            <p className="text-xs text-muted-foreground">{t(($) => $.login.hint)}</p>
          </div>
          <Button type="submit" size="lg" variant={withPasskey ? 'outline' : 'default'} disabled={busy}>
            {t(($) => $.login.submit)}
          </Button>
        </form>
      )}
      {failure !== null && (
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
          {t(($) => $.errors[failure])}
        </p>
      )}
      <Button type="button" variant="ghost" onClick={() => onRestart(false)}>
        {t(($) => $.login.back)}
      </Button>
    </div>
  )
}
