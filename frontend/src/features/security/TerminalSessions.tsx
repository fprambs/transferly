import { useQuery, useQueryClient } from '@tanstack/react-query'
import { SquareTerminal } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge, Section } from '@/components/page/Page'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Loading } from '@/components/ui/loading'
import { formatInstant } from '@/lib/datetime'
import type { CliSession } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useTimeZone } from '../auth/use-time-zone'
import { ErrorGuide } from '../errors/ErrorGuide'

import { securityErrorKeyOf, type SecurityErrorKey } from './errors'
import { cliSessionsKey } from './use-security'

/** How a session of the terminal client is named: its client and, when it said, the computer it runs on. */
function useSessionName() {
  const { t } = useTranslation('security')
  return (session: CliSession) => (session.host === '' ? session.name : t(($) => $.terminals.nameOn, { name: session.name, host: session.host }))
}

/** Props of {@link EndSessionDialog}. */
interface EndSessionDialogProps {
  /** The session to end; the dialog is closed when there is none. */
  session: CliSession | null
  /** Called when the dialog is dismissed or the session ended. */
  onClose: () => void
}

/** Asks before ending a session of the terminal client, which then has to sign in again. */
function EndSessionDialog({ session, onClose }: EndSessionDialogProps) {
  const { t } = useTranslation('security')
  const api = useApi()
  const name = useSessionName()
  const [failure, setFailure] = useState<{ key: SecurityErrorKey; error: unknown } | null>(null)
  const [busy, setBusy] = useState(false)

  const close = () => {
    setFailure(null)
    onClose()
  }
  const end = async (target: CliSession) => {
    setBusy(true)
    try {
      await api.auth.revokeSession(target.id)
      close()
    } catch (error) {
      setFailure({ key: securityErrorKeyOf(error), error })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={session !== null} onOpenChange={close}>
      <DialogContent className="sm:max-w-[420px]">
        {session !== null && (
          <div className="flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle>{t(($) => $.terminals.endTitle, { name: name(session) })}</DialogTitle>
              <DialogDescription>{t(($) => $.terminals.endBody)}</DialogDescription>
            </DialogHeader>
            {failure !== null && (
              <div role="alert" className="flex flex-col gap-2 rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
                <p>{t(($) => $.errors[failure.key])}</p>
                <ErrorGuide failure={failure.error} />
              </div>
            )}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={close}>
                {t(($) => $.password.cancel)}
              </Button>
              <Button type="button" variant="destructive" disabled={busy} onClick={() => void end(session)}>
                {t(($) => $.terminals.endConfirm)}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

/**
 * The sessions of the terminal client of the account (ADR-0029), each with the computer it said it runs on, when it started, when it
 * was last used and from where, and a button that ends it at once.
 */
export function TerminalSessions() {
  const { t, i18n } = useTranslation('security')
  const timeZone = useTimeZone()
  const api = useApi()
  const queryClient = useQueryClient()
  const name = useSessionName()
  const sessions = useQuery({ queryKey: cliSessionsKey, queryFn: () => api.auth.cliSessions() })
  const [ending, setEnding] = useState<CliSession | null>(null)
  const list = sessions.data ?? []
  const date = (instant: string) => formatInstant(instant, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })

  return (
    <>
      <Section id="terminals-title" title={t(($) => $.terminals.title)} description={t(($) => $.terminals.description)} flush>
        {sessions.isPending ? (
          <div className="px-5 py-4">
            <Loading label={t(($) => $.terminals.loading)} />
          </div>
        ) : sessions.isError ? (
          <div role="alert" className="m-5 flex flex-col gap-2 rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
            <p>{t(($) => $.errors[securityErrorKeyOf(sessions.error)])}</p>
            <ErrorGuide failure={sessions.error} />
          </div>
        ) : list.length === 0 ? (
          <div className="flex flex-col items-center gap-2 px-5 py-8 text-center">
            <span className="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground">
              <SquareTerminal aria-hidden="true" className="size-5" />
            </span>
            <p className="text-sm text-muted-foreground">{t(($) => $.terminals.empty)}</p>
          </div>
        ) : (
          <ul aria-label={t(($) => $.terminals.title)}>
            {list.map((session) => (
              <li key={session.id} className="flex flex-wrap items-center gap-3.5 border-b px-5 py-4 last:border-b-0">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/12 text-accent-text">
                  <SquareTerminal aria-hidden="true" className="size-[18px]" />
                </span>
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="truncate text-sm font-medium">{name(session)}</span>
                  <span className="text-xs text-muted-foreground">
                    {t(($) => $.terminals.detail, { started: date(session.createdAt), used: date(session.lastSeenAt), ip: session.ip })}
                  </span>
                </div>
                <div className="flex shrink-0 flex-wrap items-center gap-2">
                  {session.current && (
                    <Badge tone="ok" dot>
                      {t(($) => $.terminals.current)}
                    </Badge>
                  )}
                  <Button variant="outline" size="sm" aria-label={t(($) => $.terminals.endOf, { name: name(session) })} onClick={() => setEnding(session)}>
                    {t(($) => $.terminals.end)}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Section>
      <EndSessionDialog
        session={ending}
        onClose={() => {
          setEnding(null)
          void queryClient.invalidateQueries({ queryKey: cliSessionsKey })
        }}
      />
    </>
  )
}
