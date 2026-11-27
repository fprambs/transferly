import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { sessionKey, sessionQuery, setupKey } from '@/app/session'
import { AuthFrame } from '@/components/brand/AuthFrame'
import { BrandMark } from '@/components/brand/BrandMark'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Loading } from '@/components/ui/loading'
import { formatInstant } from '@/lib/datetime'
import type { InvitationPreview, MfaChallenge, SessionInfo } from '@/lib/api'
import { LegalLink } from '@/features/legal/LegalPage'
import { useApi } from '@/lib/api/use-api'
import { useTransport } from '@/lib/transport'
import { useUiStore } from '@/state/ui-store'

import { MfaStep } from '../security/MfaStep'
import { needsSecondStep } from '../security/errors'
import { workspacesKey } from '../workspace/use-workspace'

import { teamErrorKeyOf, type TeamErrorKey } from './errors'
import { TeamFailure } from './TeamMessage'

/** Minimum length of an account password. */
const minPasswordLength = 12

/** Frame shared by every state of the page: the brand, a title and the content. */
function Card({ title, children }: { title: string; children?: ReactNode }) {
  const { t: common } = useTranslation()
  return (
    <AuthFrame width="max-w-md">
      <div className="flex flex-col gap-1.5">
        <p className="flex items-center gap-2 text-base font-bold tracking-tight lg:hidden">
          <BrandMark className="size-7" />
          {common(($) => $.app.name)}
        </p>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
      </div>
      {children}
      <LegalLink />
    </AuthFrame>
  )
}

/** Props of the forms and panels that act on an invitation. */
interface InvitationProps {
  /** Secret of the invitation. */
  token: string
  /** What the invitation offers. */
  preview: InvitationPreview
}

/** Stores a fresh session: the token of the transport, the session query and the fact that setup is done. */
function useStartSession() {
  const transport = useTransport()
  const queryClient = useQueryClient()
  return (session: SessionInfo) => {
    transport.setCsrfToken(session.csrfToken)
    queryClient.setQueryData(sessionKey, session)
    queryClient.setQueryData(setupKey, false)
  }
}

/** Opens the members page of a workspace once its invitation was used. */
function useEnter() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const setWorkspace = useUiStore((state) => state.setWorkspace)
  return async (workspaceId: string | undefined) => {
    await queryClient.invalidateQueries({ queryKey: workspacesKey })
    if (workspaceId !== undefined) {
      setWorkspace(workspaceId)
    }
    await navigate({ to: '/', search: { settings: 'members' } })
  }
}

/** Facts of the invitation: the address, the role and the expiry. */
function Summary({ preview }: { preview: InvitationPreview }) {
  const { t, i18n } = useTranslation('teams')
  return (
    <>
      <p className="text-sm leading-relaxed text-muted-foreground">
        {preview.invitedBy !== undefined && `${t(($) => $.accept.by, { email: preview.invitedBy })} `}
        {t(($) => $.accept.asRole, { role: t(($) => $.roles[preview.role]) })}
      </p>
      <dl className="flex flex-col gap-1.5 text-sm">
        <div className="flex justify-between border-b pb-1.5">
          <dt className="text-muted-foreground">{t(($) => $.accept.for)}</dt>
          <dd>{preview.email}</dd>
        </div>
        <div className="flex justify-between border-b pb-1.5">
          <dt className="text-muted-foreground">{t(($) => $.accept.expires)}</dt>
          <dd>{formatInstant(preview.expiresAt, { locale: i18n.language, timeZone: 'UTC', dateStyle: 'medium' })}</dd>
        </div>
      </dl>
    </>
  )
}

/** Form for an address without an account: choose a password, create the account and join. */
function JoinForm({ token, preview }: InvitationProps) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const startSession = useStartSession()
  const enter = useEnter()
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [failure, setFailure] = useState<TeamErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (password.length < minPasswordLength) {
      setFailure('passwordShort')
      return
    }
    if (password !== repeat) {
      setFailure('passwordMismatch')
      return
    }
    setBusy(true)
    try {
      startSession(await api.invitations.join(token, password))
      const joined = (await api.workspaces.list()).find((workspace) => workspace.kind === 'team' && workspace.name === preview.workspaceName)
      await enter(joined?.id)
    } catch (error) {
      setFailure(teamErrorKeyOf(error))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="invite-account-email">{t(($) => $.accept.email)}</Label>
        <Input id="invite-account-email" type="email" value={preview.email} readOnly autoComplete="username" aria-describedby="invite-fixed" />
        <p id="invite-fixed" className="text-xs text-muted-foreground">
          {t(($) => $.accept.fixed)}
        </p>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="invite-password">{t(($) => $.accept.password)}</Label>
        <Input id="invite-password" type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} />
        <p className="text-xs text-muted-foreground">{t(($) => $.accept.passwordHint)}</p>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="invite-repeat">{t(($) => $.accept.repeat)}</Label>
        <Input id="invite-repeat" type="password" autoComplete="new-password" value={repeat} onChange={(event) => setRepeat(event.target.value)} />
      </div>
      <p className="rounded-lg border border-sky-500/30 bg-sky-500/10 px-3 py-2 text-sm leading-relaxed">{t(($) => $.accept.vaultNote)}</p>
      <TeamFailure failure={failure} />
      <Button type="submit" size="lg" disabled={busy}>
        {t(($) => $.accept.join)}
      </Button>
    </form>
  )
}

/** Form for an address that already has an account: sign in and join. */
function SignInForm({ token, preview }: InvitationProps) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const startSession = useStartSession()
  const enter = useEnter()
  const [password, setPassword] = useState('')
  const [challenge, setChallenge] = useState<MfaChallenge | null>(null)
  const [failure, setFailure] = useState<TeamErrorKey | null>(null)
  const [busy, setBusy] = useState(false)

  const finish = async (session: SessionInfo) => {
    startSession(session)
    await enter((await api.invitations.accept(token)).id)
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      const answer = await api.auth.login({ email: preview.email, password })
      if (needsSecondStep(answer)) {
        setChallenge(answer)
        return
      }
      await finish(answer)
    } catch (error) {
      setFailure(teamErrorKeyOf(error))
      setPassword('')
      setBusy(false)
    }
  }

  if (challenge !== null) {
    return (
      <MfaStep
        challenge={challenge}
        onVerified={finish}
        onRestart={(expired) => {
          setChallenge(null)
          setBusy(false)
          setFailure(expired ? 'invalid_challenge' : null)
        }}
      />
    )
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="invite-signin-email">{t(($) => $.accept.email)}</Label>
        <Input id="invite-signin-email" type="email" value={preview.email} readOnly autoComplete="username" />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="invite-signin-password">{t(($) => $.accept.password)}</Label>
        <Input id="invite-signin-password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} />
      </div>
      <TeamFailure failure={failure} />
      <Button type="submit" size="lg" disabled={busy}>
        {t(($) => $.accept.signIn)}
      </Button>
    </form>
  )
}

/** Panel for somebody who is signed in: join when the account is the invited one, else explain and offer to sign out. */
function SignedIn({ token, preview, session }: InvitationProps & { session: SessionInfo }) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const transport = useTransport()
  const queryClient = useQueryClient()
  const enter = useEnter()
  const [failure, setFailure] = useState<TeamErrorKey | null>(null)

  if (session.user.email !== preview.email) {
    return (
      <>
        <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
          {t(($) => $.accept.wrongAccount, { email: preview.email, current: session.user.email })}
        </p>
        <Button
          variant="outline"
          onClick={async () => {
            await api.auth.logout()
            transport.setCsrfToken(undefined)
            queryClient.setQueryData(sessionKey, null)
          }}
        >
          {t(($) => $.accept.signOut)}
        </Button>
      </>
    )
  }
  return (
    <>
      <p className="text-sm text-muted-foreground">{t(($) => $.accept.signedInAs, { email: session.user.email })}</p>
      <TeamFailure failure={failure} />
      <Button
        size="lg"
        onClick={async () => {
          try {
            await enter((await api.invitations.accept(token)).id)
          } catch (error) {
            setFailure(teamErrorKeyOf(error))
          }
        }}
      >
        {t(($) => $.accept.accept, { name: preview.workspaceName })}
      </Button>
    </>
  )
}

/** The page an invitation link opens: what it offers, and the way to join it as the right account. */
export function AcceptInvitationPage() {
  const { t } = useTranslation('teams')
  const api = useApi()
  const transport = useTransport()
  const token = useLocation({ select: (location) => location.hash })
  const preview = useQuery({ queryKey: ['invitation', token], queryFn: () => api.invitations.preview(token), enabled: token !== '', retry: false })
  const session = useQuery(sessionQuery(api, transport))

  if (token === '') {
    return (
      <Card title={t(($) => $.accept.invalidTitle)}>
        <p role="alert" className="text-sm text-muted-foreground">
          {t(($) => $.accept.missing)}
        </p>
      </Card>
    )
  }
  if (preview.isError) {
    return (
      <Card title={t(($) => $.accept.invalidTitle)}>
        <p role="alert" className="text-sm text-muted-foreground">
          {t(($) => $.errors[teamErrorKeyOf(preview.error) === 'rate_limited' ? 'rate_limited' : 'invitation_not_found'])}
        </p>
        <p className="text-xs text-muted-foreground">{t(($) => $.accept.sameHint)}</p>
      </Card>
    )
  }
  if (preview.data === undefined || session.isPending) {
    return (
      <Card title={t(($) => $.accept.label)}>
        <Loading label={t(($) => $.accept.loading)} />
      </Card>
    )
  }

  const offer = preview.data
  return (
    <Card title={t(($) => $.accept.title, { name: offer.workspaceName })}>
      <Summary preview={offer} />
      {session.data ? (
        <SignedIn token={token} preview={offer} session={session.data} />
      ) : offer.accountExists ? (
        <SignInForm token={token} preview={offer} />
      ) : (
        <JoinForm token={token} preview={offer} />
      )}
      <p className="text-xs text-muted-foreground">{t(($) => $.accept.sameHint)}</p>
    </Card>
  )
}
