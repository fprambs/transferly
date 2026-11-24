import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Fingerprint, KeyRound, Smartphone, type LucideIcon } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge, Section } from '@/components/page/Page'
import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { formatInstant } from '@/lib/datetime'
import type { Passkey } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useTimeZone } from '../auth/use-time-zone'

import { AddPasskeyDialog } from './AddPasskeyDialog'
import { EnableTotpDialog } from './EnableTotpDialog'
import { PasswordDialog, type PasswordAction } from './PasswordDialog'
import { RemovePasskeyDialog } from './RemovePasskeyDialog'
import { TerminalSessions } from './TerminalSessions'
import { securityKey } from './use-security'

/** Props of {@link MethodRow}. */
interface MethodRowProps {
  /** Icon of the method, in a tinted tile. */
  icon: LucideIcon
  /** Name of the method. */
  title: ReactNode
  /** How it works or how it stands, under the name. */
  detail: ReactNode
  /** State and buttons on the right. */
  children?: ReactNode
}

/** One way of signing in, as a row of a card: its icon, name and detail, and its state and actions on the right. */
function MethodRow({ icon: Icon, title, detail, children }: MethodRowProps) {
  return (
    <div className="flex flex-wrap items-center gap-3.5 border-b px-5 py-4 last:border-b-0">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/12 text-accent-text">
        <Icon aria-hidden="true" className="size-[18px]" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-medium">{title}</span>
        <span className="text-xs text-muted-foreground">{detail}</span>
      </div>
      {children !== undefined && <div className="flex shrink-0 flex-wrap items-center gap-2">{children}</div>}
    </div>
  )
}

/**
 * The second step of the account, in two cards: the authenticator app with its recovery codes, turned on and off with the password, and
 * the passkeys, each in its own row with when it was added and last used; then the sessions of the terminal client.
 */
export function SecuritySection() {
  const { t, i18n } = useTranslation('security')
  const timeZone = useTimeZone()
  const api = useApi()
  const queryClient = useQueryClient()
  const status = useQuery({ queryKey: securityKey, queryFn: () => api.auth.security() })
  const [enabling, setEnabling] = useState(false)
  const [action, setAction] = useState<PasswordAction | null>(null)
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<Passkey | null>(null)
  const on = status.data?.totp === true
  const passkeys = status.data?.passkeys ?? []
  const refresh = () => void queryClient.invalidateQueries({ queryKey: securityKey })
  const date = (instant: string) => formatInstant(instant, { locale: i18n.language, timeZone, dateStyle: 'medium' })

  return (
    <>
      <Section id="security-title" title={t(($) => $.section.title)} description={t(($) => $.section.description)} flush>
        {status.isPending ? (
          <div className="px-5 py-4">
            <Loading label={t(($) => $.section.loading)} />
          </div>
        ) : (
          <>
            <MethodRow icon={Smartphone} title={t(($) => $.section.app)} detail={t(($) => $.section.appDetail)}>
              <Badge tone={on ? 'ok' : 'muted'} dot>
                {on ? t(($) => $.section.statusOn) : t(($) => $.section.statusOff)}
              </Badge>
              {on ? (
                <Button variant="outline" size="sm" onClick={() => setAction('disable')}>
                  {t(($) => $.section.disable)}
                </Button>
              ) : (
                <Button size="sm" onClick={() => setEnabling(true)}>
                  {t(($) => $.section.enable)}
                </Button>
              )}
            </MethodRow>
            {on && (
              <MethodRow icon={KeyRound} title={t(($) => $.section.recoveryTitle)} detail={t(($) => $.section.recoveryLeft, { count: status.data?.recoveryCodesLeft })}>
                <Button variant="outline" size="sm" onClick={() => setAction('regenerate')}>
                  {t(($) => $.section.regenerate)}
                </Button>
              </MethodRow>
            )}
          </>
        )}
      </Section>
      <Section
        id="passkeys-title"
        title={t(($) => $.passkeys.title)}
        description={t(($) => $.passkeys.description)}
        actions={
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            {t(($) => $.passkeys.add)}
          </Button>
        }
        flush
      >
        {passkeys.length === 0 ? (
          <div className="flex flex-col items-center gap-2 px-5 py-8 text-center">
            <span className="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground">
              <Fingerprint aria-hidden="true" className="size-5" />
            </span>
            <p className="text-sm text-muted-foreground">{t(($) => $.passkeys.empty)}</p>
          </div>
        ) : (
          <ul aria-label={t(($) => $.passkeys.title)}>
            {passkeys.map((passkey) => (
              <li key={passkey.id}>
                <MethodRow
                  icon={Fingerprint}
                  title={passkey.name}
                  detail={
                    <>
                      {t(($) => $.passkeys.created, { date: date(passkey.createdAt) })} ·{' '}
                      {passkey.lastUsedAt === undefined ? t(($) => $.passkeys.neverUsed) : t(($) => $.passkeys.used, { date: date(passkey.lastUsedAt) })}
                    </>
                  }
                >
                  <Button variant="outline" size="sm" aria-label={t(($) => $.passkeys.removeOf, { name: passkey.name })} onClick={() => setRemoving(passkey)}>
                    {t(($) => $.passkeys.remove)}
                  </Button>
                </MethodRow>
              </li>
            ))}
          </ul>
        )}
      </Section>
      <TerminalSessions />
      <EnableTotpDialog
        open={enabling}
        onClose={() => {
          setEnabling(false)
          refresh()
        }}
      />
      <PasswordDialog
        action={action}
        onClose={() => {
          setAction(null)
          refresh()
        }}
      />
      <AddPasskeyDialog
        open={adding}
        onClose={() => {
          setAdding(false)
          refresh()
        }}
      />
      <RemovePasskeyDialog
        passkey={removing}
        onClose={() => {
          setRemoving(null)
          refresh()
        }}
      />
    </>
  )
}
