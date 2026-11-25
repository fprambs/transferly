import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { AuthFrame } from '@/components/brand/AuthFrame'
import { BrandMark } from '@/components/brand/BrandMark'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useApi } from '@/lib/api/use-api'

import { useSession } from '../auth/use-session'
import { ErrorGuide } from '../errors/ErrorGuide'
import { securityErrorKeyOf, type SecurityErrorKey } from '../security/errors'

import { callbackUrl, isWellFormed, leaveTo, type CliAuthorizeRequest } from './request'

/** The route of the page, to read its search parameters. */
const route = getRouteApi('/cli/authorize')

/** The route of a short link, to read its code. */
const linkRoute = getRouteApi('/cli/a/$code')

/** What the page shows once the person decided. */
type Outcome = { kind: 'deciding' } | { kind: 'returning' } | { kind: 'code'; code: string } | { kind: 'denied' }

/** A failure to explain under the form: its message and, for failures of the server, the guidance of its code. */
interface Failure {
  /** Translation key of the message. */
  key: SecurityErrorKey
  /** The failed request, if any. */
  error?: unknown
}

/** Frame of every state of the page: the brand, a title and the content. */
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
    </AuthFrame>
  )
}

/** The single-use code for the person to paste into the terminal, with a button to copy it. */
function CodePanel({ code }: { code: string }) {
  const { t } = useTranslation('security')
  const [copied, setCopied] = useState(false)
  return (
    <Card title={t(($) => $.cli.approvedTitle)}>
      <p className="text-sm text-muted-foreground">{t(($) => $.cli.codeBody)}</p>
      <code aria-label={t(($) => $.cli.codeLabel)} className="rounded-md border bg-muted px-3 py-2 font-mono text-sm break-all select-all">
        {code}
      </code>
      <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm leading-relaxed">{t(($) => $.cli.codeWarning)}</p>
      <Button
        type="button"
        onClick={async () => {
          await navigator.clipboard.writeText(code)
          setCopied(true)
        }}
      >
        {copied ? t(($) => $.cli.copied) : t(($) => $.cli.copy)}
      </Button>
    </Card>
  )
}

/** The form that shows who asks and approves with the password, or denies. */
function Approval({ request }: { request: CliAuthorizeRequest }) {
  const { t } = useTranslation('security')
  const api = useApi()
  const session = useSession()
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<Failure | null>(null)
  const [outcome, setOutcome] = useState<Outcome>({ kind: 'deciding' })
  const redirect = request.redirect_uri === '' ? undefined : request.redirect_uri
  const client = request.client_name.trim()
  const host = request.client_host?.trim() ?? ''

  const approve = async (event: FormEvent) => {
    event.preventDefault()
    if (password === '') {
      setFailure({ key: 'passwordRequired' })
      return
    }
    setBusy(true)
    setFailure(null)
    try {
      const { code } = await api.auth.approveCli({ codeChallenge: request.code_challenge, redirectUri: redirect, clientName: client, clientHost: host, password })
      if (redirect === undefined) {
        setOutcome({ kind: 'code', code })
      } else {
        setOutcome({ kind: 'returning' })
        leaveTo(callbackUrl(redirect, { code }, request.state))
      }
    } catch (error) {
      setFailure({ key: securityErrorKeyOf(error), error })
      setPassword('')
    } finally {
      setBusy(false)
    }
  }

  const deny = () => {
    setOutcome({ kind: 'denied' })
    if (redirect !== undefined) {
      leaveTo(callbackUrl(redirect, { error: 'access_denied' }, request.state))
    }
  }

  switch (outcome.kind) {
    case 'code':
      return <CodePanel code={outcome.code} />
    case 'returning':
      return (
        <Card title={t(($) => $.cli.approvedTitle)}>
          <p className="text-sm text-muted-foreground">{t(($) => $.cli.returningBody)}</p>
        </Card>
      )
    case 'denied':
      return (
        <Card title={t(($) => $.cli.deniedTitle)}>
          <p className="text-sm text-muted-foreground">{t(($) => $.cli.deniedBody)}</p>
        </Card>
      )
  }

  return (
    <Card title={t(($) => $.cli.title)}>
      <form onSubmit={approve} noValidate className="flex flex-col gap-4">
        <p className="text-sm leading-relaxed">
          {host === ''
            ? t(($) => $.cli.introNoHost, { client, email: session?.user.email })
            : t(($) => $.cli.intro, { client, host, email: session?.user.email })}
        </p>
        <p className="text-xs text-muted-foreground">{t(($) => $.cli.declared)}</p>
        <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm leading-relaxed">{t(($) => $.cli.warning)}</p>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="cli-password">{t(($) => $.cli.password)}</Label>
          <Input id="cli-password" type="password" autoComplete="current-password" autoFocus value={password} onChange={(event) => setPassword(event.target.value)} />
        </div>
        {failure !== null && (
          <div role="alert" className="flex flex-col gap-2 rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
            <p>{t(($) => $.errors[failure.key])}</p>
            {failure.error !== undefined && <ErrorGuide failure={failure.error} />}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={deny} disabled={busy}>
            {t(($) => $.cli.deny)}
          </Button>
          <Button type="submit" disabled={busy}>
            {busy ? t(($) => $.cli.working) : t(($) => $.cli.approve)}
          </Button>
        </div>
      </form>
    </Card>
  )
}

/**
 * Where the person approves or denies a sign-in of the terminal client (ADR-0029). It shows what the client declares, asks for the
 * password again, and hands the single-use code back to the terminal through its loopback address, or shows it to paste.
 */
export function CliAuthorizePage() {
  return <Checked request={route.useSearch()} />
}

/**
 * The approval page of a short link, /cli/a/<code>, which the terminal shows because it fits on one line and can be typed on another
 * device: the request is loaded from the server and shown as /cli/authorize shows it. An unknown or expired link is not valid.
 */
export function CliLinkPage() {
  const { t } = useTranslation('security')
  const api = useApi()
  const { code } = linkRoute.useParams()
  const link = useQuery({ queryKey: ['cli-link', code], queryFn: () => api.auth.cliLink(code), retry: false, staleTime: Infinity })
  if (link.isPending) {
    return <Card title={t(($) => $.cli.loading)} />
  }
  if (link.isError) {
    return <Invalid />
  }
  const { codeChallenge, redirectUri, state, clientName, clientHost } = link.data
  return <Checked request={{ code_challenge: codeChallenge, redirect_uri: redirectUri, state, client_name: clientName, client_host: clientHost }} />
}

/** A request that is well formed is shown to approve; any other says the link is not valid. */
function Checked({ request }: { request: CliAuthorizeRequest }) {
  return isWellFormed(request) ? <Approval request={request} /> : <Invalid />
}

/** Says the sign-in link is not valid and how to get a new one. */
function Invalid() {
  const { t } = useTranslation('security')
  return (
    <Card title={t(($) => $.cli.invalidTitle)}>
      <p className="text-sm text-muted-foreground">{t(($) => $.cli.invalidBody)}</p>
    </Card>
  )
}
