import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useApi } from '@/lib/api/use-api'

import { EnableTotpDialog } from './EnableTotpDialog'
import { PasswordDialog, type PasswordAction } from './PasswordDialog'
import { securityKey } from './use-security'

/** The settings section for the second factor: whether it is on, and how to turn it on or off and renew the recovery codes. */
export function SecuritySection() {
  const { t } = useTranslation('security')
  const api = useApi()
  const queryClient = useQueryClient()
  const status = useQuery({ queryKey: securityKey, queryFn: () => api.auth.security() })
  const [enabling, setEnabling] = useState(false)
  const [action, setAction] = useState<PasswordAction | null>(null)
  const on = status.data?.totp === true

  return (
    <section aria-labelledby="security-title" className="flex max-w-xl flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h3 id="security-title" className="text-sm font-semibold">
          {t(($) => $.section.title)}
        </h3>
        <p className="text-sm text-muted-foreground">{t(($) => $.section.description)}</p>
      </div>
      {status.isPending ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.section.loading)}</p>
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
      <EnableTotpDialog
        open={enabling}
        onClose={() => {
          setEnabling(false)
          void queryClient.invalidateQueries({ queryKey: securityKey })
        }}
      />
      <PasswordDialog
        action={action}
        onClose={() => {
          setAction(null)
          void queryClient.invalidateQueries({ queryKey: securityKey })
        }}
      />
    </section>
  )
}
