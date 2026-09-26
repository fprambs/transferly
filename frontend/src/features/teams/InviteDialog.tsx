import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatInstant } from '@/lib/datetime'
import type { GrantableRole, InvitationCreated, Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'

import { useTimeZone } from '../auth/use-time-zone'

import { invitationLink } from './invitation-link'
import { teamErrorKeyOf, type TeamErrorKey } from './errors'
import { grantableRoles } from './roles'
import { teamKeys } from './use-teams'
import { TeamFailure } from './TeamMessage'

/** Props of {@link InviteDialog}. */
export interface InviteDialogProps {
  /** The team workspace to invite to; the dialog is closed when there is none. */
  workspace: Workspace | null
  /** Called when the dialog is dismissed. */
  onClose: () => void
}

/** Props of the two steps of the dialog. */
interface StepProps {
  /** The workspace the invitation is for. */
  workspace: Workspace
  /** Called when the dialog is dismissed. */
  onClose: () => void
}

/** Second step: the one-time link and what it grants. */
function InviteResult({ workspace, created, onClose }: StepProps & { created: InvitationCreated }) {
  const { t, i18n } = useTranslation('teams')
  const timeZone = useTimeZone()
  const [copied, setCopied] = useState(false)
  const link = invitationLink(window.location.origin, created.token)

  return (
    <div className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.invite.readyTitle)}</DialogTitle>
        <DialogDescription>{t(($) => $.invite.step2)}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="invite-link">{t(($) => $.invite.link)}</Label>
        <div className="flex gap-2">
          <Input id="invite-link" readOnly value={link} className="font-mono text-xs" onFocus={(event) => event.currentTarget.select()} />
          <Button
            type="button"
            onClick={async () => {
              await navigator.clipboard.writeText(link)
              setCopied(true)
            }}
          >
            {copied ? t(($) => $.invite.copied) : t(($) => $.invite.copy)}
          </Button>
        </div>
      </div>
      <dl className="grid grid-cols-3 gap-2.5 text-sm">
        <div className="rounded-lg border px-3 py-2">
          <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t(($) => $.invite.for)}</dt>
          <dd className="mt-1 truncate">{created.invitation.email}</dd>
        </div>
        <div className="rounded-lg border px-3 py-2">
          <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t(($) => $.invite.roleLabel)}</dt>
          <dd className="mt-1">{t(($) => $.roles[created.invitation.role])}</dd>
        </div>
        <div className="rounded-lg border px-3 py-2">
          <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t(($) => $.invite.expires)}</dt>
          <dd className="mt-1">{formatInstant(created.invitation.expiresAt, { locale: i18n.language, timeZone, dateStyle: 'medium' })}</dd>
        </div>
      </dl>
      <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm leading-relaxed">
        {t(($) => $.invite.onlyOnce, { email: created.invitation.email, name: workspace.name })}
      </p>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.invite.done)}
        </Button>
      </DialogFooter>
    </div>
  )
}

/** First step: the address and the role. */
function InviteForm({ workspace, onClose, onCreated }: StepProps & { onCreated: (created: InvitationCreated) => void }) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const queryClient = useQueryClient()
  const options = grantableRoles(workspace.role)
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<GrantableRole>('editor')
  const [failure, setFailure] = useState<TeamErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (email.trim() === '') {
      setFailure('emailRequired')
      return
    }
    setBusy(true)
    try {
      const created = await api.invitations.create(workspace.id, { email: email.trim(), role })
      await queryClient.invalidateQueries({ queryKey: teamKeys.invitations(workspace.id) })
      onCreated(created)
    } catch (error) {
      setFailure(teamErrorKeyOf(error))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.invite.title, { name: workspace.name })}</DialogTitle>
        <DialogDescription>{t(($) => $.invite.step1)}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="invite-email">{t(($) => $.invite.email)}</Label>
        <Input id="invite-email" type="email" autoComplete="off" autoFocus value={email} onChange={(event) => setEmail(event.target.value)} />
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-sm font-medium">{t(($) => $.invite.role)}</legend>
        {options.map((option) => (
          <label
            key={option}
            className={cn(
              'flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 has-focus-visible:ring-2 has-focus-visible:ring-ring',
              role === option ? 'border-primary bg-primary/10' : 'border-border',
            )}
          >
            <input type="radio" name="invite-role" value={option} checked={role === option} onChange={() => setRole(option)} className="mt-1 accent-primary" />
            <span className="flex flex-col text-sm leading-snug">
              <span className="font-medium">{t(($) => $.roles[option])}</span>
              <span className="text-xs text-muted-foreground">{t(($) => $.roleHelp[option])}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <TeamFailure failure={failure} />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t(($) => $.invite.cancel)}
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? t(($) => $.invite.creating) : t(($) => $.invite.create)}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** Body of the dialog: the form until an invitation exists, then its link. */
function InviteBody({ workspace, onClose }: StepProps) {
  const [created, setCreated] = useState<InvitationCreated | null>(null)
  return created === null ? (
    <InviteForm workspace={workspace} onClose={onClose} onCreated={setCreated} />
  ) : (
    <InviteResult workspace={workspace} created={created} onClose={onClose} />
  )
}

/** Invites an email address to a team workspace in two steps and shows the one-time link. */
export function InviteDialog({ workspace, onClose }: InviteDialogProps) {
  return (
    <Dialog open={workspace !== null} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[520px]">{workspace !== null && <InviteBody workspace={workspace} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}
