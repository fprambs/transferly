import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

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
import { securityKey } from './use-security'

/** The settings section for the second factor: whether it is on, and how to turn it on or off and renew the recovery codes. */
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

  return (
    <section aria-labelledby="security-title" className="flex max-w-xl flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h3 id="security-title" className="text-sm font-semibold">
          {t(($) => $.section.title)}
        </h3>
        <p className="text-sm text-muted-foreground">{t(($) => $.section.description)}</p>
      </div>
      {status.isPending ? (
        <Loading label={t(($) => $.section.loading)} />
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <span className={on ? 'rounded-full bg-emerald-500/15 px-2.5 py-0.5 text-xs font-medium text-emerald-800 dark:text-emerald-300' : 'rounded-full bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground'}>
            {on ? t(($) => $.section.statusOn) : t(($) => $.section.statusOff)}
          </span>
          {on && <span className="text-sm text-muted-foreground">{t(($) => $.section.recoveryLeft, { count: status.data?.recoveryCodesLeft })}</span>}
          {on ? (
            <>
              <Button variant="outline" size="sm" onClick={() => setAction('regenerate')}>
                {t(($) => $.section.regenerate)}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setAction('disable')}>
                {t(($) => $.section.disable)}
              </Button>
            </>
          ) : (
            <Button size="sm" onClick={() => setEnabling(true)}>
              {t(($) => $.section.enable)}
            </Button>
          )}
        </div>
      )}
      <div className="flex flex-col gap-2 pt-2">
        <div className="flex items-center gap-3">
          <h4 className="text-sm font-medium">{t(($) => $.passkeys.title)}</h4>
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            {t(($) => $.passkeys.add)}
          </Button>
        </div>
        <p className="text-sm text-muted-foreground">{t(($) => $.passkeys.description)}</p>
        {passkeys.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t(($) => $.passkeys.empty)}</p>
        ) : (
          <ul aria-label={t(($) => $.passkeys.title)} className="overflow-hidden rounded-lg border bg-card">
            {passkeys.map((passkey) => (
              <li key={passkey.id} className="flex items-center gap-3 border-b px-3 py-2 text-sm last:border-b-0">
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{passkey.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {t(($) => $.passkeys.created, { date: formatInstant(passkey.createdAt, { locale: i18n.language, timeZone, dateStyle: 'medium' }) })} ·{' '}
                    {passkey.lastUsedAt === undefined
                      ? t(($) => $.passkeys.neverUsed)
                      : t(($) => $.passkeys.used, { date: formatInstant(passkey.lastUsedAt, { locale: i18n.language, timeZone, dateStyle: 'medium' }) })}
                  </span>
                </span>
                <Button variant="outline" size="sm" aria-label={t(($) => $.passkeys.removeOf, { name: passkey.name })} onClick={() => setRemoving(passkey)}>
                  {t(($) => $.passkeys.remove)}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
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
    </section>
  )
}
