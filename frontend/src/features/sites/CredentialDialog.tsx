import { KeyRound } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useApi } from '@/lib/api/use-api'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import type { VaultTarget } from '../vault/VaultDialog'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'

/** A site that asks for its password when connecting, named in the dialog. */
export interface CredentialTarget extends VaultTarget {
  /** Identifier of the site. */
  id: string
}

/** Props of {@link CredentialDialog}. */
export interface CredentialDialogProps {
  /** Workspace of the site. */
  workspaceId: string
  /** The site whose password is asked for, or `null` when the dialog is closed. */
  target: CredentialTarget | null
  /** Called when the dialog is dismissed without a password. */
  onClose: () => void
  /** Called once the password is held for the session, so the caller can connect again. */
  onGiven: () => void
  /** Whether the server just refused the password given before, which the dialog says. */
  refused?: boolean
}

/** The password form inside {@link CredentialDialog}. Its submit event stops here, so a dialog opened from another form never submits it too. */
function CredentialForm({ workspaceId, target, onClose, onGiven, refused = false }: CredentialDialogProps & { target: CredentialTarget }) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(true)
  const [failure, setFailure] = useState<SiteErrorKey | 'passwordRequired' | 'refused' | null>(refused ? 'refused' : null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    event.stopPropagation()
    if (password === '') {
      setFailure('passwordRequired')
      return
    }
    setBusy(true)
    setFailure(null)
    try {
      await api.sites.setCredential(workspaceId, target.id, password, remember)
      onGiven()
    } catch (error) {
      setFailure(siteErrorKeyOf(error))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={(event) => void submit(event)} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.credential.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.credential.description)}</DialogDescription>
      </DialogHeader>
      <dl aria-label={t(($) => $.credential.target)} className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-lg border bg-muted/40 px-3 py-2.5 text-sm">
        <dt className="text-muted-foreground">{t(($) => $.site.name)}</dt>
        <dd className="truncate font-medium">{target.name}</dd>
        <dt className="text-muted-foreground">{t(($) => $.site.host)}</dt>
        <dd className="truncate font-mono">{target.host}</dd>
        <dt className="text-muted-foreground">{t(($) => $.site.username)}</dt>
        <dd className="truncate font-mono">{target.username}</dd>
      </dl>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="site-credential-password">{t(($) => $.site.password)}</Label>
        <Input id="site-credential-password" type="password" autoFocus autoComplete="off" value={password} onChange={(event) => setPassword(event.target.value)} />
      </div>
      <div className="flex items-start gap-3">
        <Checkbox id="site-credential-remember" aria-labelledby="site-credential-remember-label" checked={remember} onCheckedChange={setRemember} />
        <div className="flex flex-col text-sm">
          <span id="site-credential-remember-label" className="font-medium">
            {t(($) => $.credential.remember)}
          </span>
          <span className="text-muted-foreground">{t(($) => $.credential.rememberHint)}</span>
        </div>
      </div>
      {failure !== null && (
        <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <p>{failure === 'passwordRequired' || failure === 'refused' ? t(($) => $.credential[failure]) : t(($) => $.errors[failure])}</p>
          <ErrorGuide failure={failure} className="mt-2" />
        </div>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.site.cancel)}
        </Button>
        <Button type="submit" disabled={busy}>
          <KeyRound />
          {t(($) => $.credential.submit)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/**
 * Asks for the password of a site that does not store it (ADR-0010). The password goes to the server, which holds it in memory for this session
 * only; it is never stored in the browser either.
 */
export function CredentialDialog({ target, ...props }: CredentialDialogProps) {
  return (
    <Dialog open={target !== null} onOpenChange={props.onClose}>
      <DialogContent className="sm:max-w-[480px]">{target !== null && <CredentialForm key={target.id} target={target} {...props} />}</DialogContent>
    </Dialog>
  )
}
