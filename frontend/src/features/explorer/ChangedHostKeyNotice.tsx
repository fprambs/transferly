import { ShieldAlert } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'
import type { HostKeyTrustMode } from '@/lib/api'

import type { ChangedHostKey } from '../quick/errors'

import type { ExplorerErrorKey } from './errors'

/** Props of {@link ChangedHostKeyNotice}. */
export interface ChangedHostKeyNoticeProps {
  /** The key the server presented and the ones trusted before it. */
  hostKey: ChangedHostKey
  /** Why the last decision failed, if it did. */
  failure: ExplorerErrorKey | null
  /** Called with the decision to trust the new key next to the trusted ones or instead of them. */
  onTrust: (mode: HostKeyTrustMode) => Promise<void>
  /** Called when the user does not trust the key, to disconnect the site; without it that choice is not offered. */
  onCancel?: () => void
}

/**
 * Warns that a server presented a key other than the ones trusted for it (4.13b): it shows the new fingerprint below the trusted ones and
 * lets the user add it (a service whose servers each have their own key), replace them (a server whose key really changed) or cancel. There
 * is no way to connect without deciding, and every decision is audited on the server.
 */
export function ChangedHostKeyNotice({ hostKey, failure, onTrust, onCancel }: ChangedHostKeyNoticeProps) {
  const { t } = useTranslation('explorer')
  const [busy, setBusy] = useState(false)

  const decide = async (mode: HostKeyTrustMode): Promise<void> => {
    setBusy(true)
    try {
      await onTrust(mode)
    } finally {
      setBusy(false)
    }
  }

  return (
    <PanelState icon={ShieldAlert} tone="danger" role="alert" title={t(($) => $.changedKey.title)} description={t(($) => $.changedKey.body)} className="max-w-xl">
      <dl className="grid w-full grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 rounded-lg border bg-muted/40 px-4 py-2 text-start text-sm">
        <dt className="text-muted-foreground">{t(($) => $.trust.keyType)}</dt>
        <dd className="font-mono">{hostKey.keyType}</dd>
        <dt className="text-muted-foreground">{t(($) => $.changedKey.trusted, { count: hostKey.trusted.length })}</dt>
        <dd>
          <ul className="flex flex-col gap-0.5">
            {hostKey.trusted.map((fingerprint) => (
              <li key={fingerprint} className="font-mono break-all">
                {fingerprint}
              </li>
            ))}
          </ul>
        </dd>
        <dt className="font-medium text-destructive">{t(($) => $.changedKey.presented)}</dt>
        <dd className="font-mono font-medium break-all text-destructive">{hostKey.fingerprint}</dd>
      </dl>
      <p className="text-start text-xs leading-relaxed text-muted-foreground">{t(($) => $.changedKey.help)}</p>
      {failure !== null && <p className="text-sm text-destructive">{t(($) => $.errors[failure])}</p>}
      <div className="flex flex-wrap justify-center gap-2">
        {onCancel !== undefined && (
          <Button variant="outline" disabled={busy} onClick={onCancel}>
            {t(($) => $.changedKey.cancel)}
          </Button>
        )}
        <Button variant="secondary" disabled={busy} onClick={() => void decide('add')}>
          {t(($) => $.changedKey.add)}
        </Button>
        <Button variant="destructive" disabled={busy} onClick={() => void decide('replace')}>
          {t(($) => $.changedKey.replace)}
        </Button>
      </div>
    </PanelState>
  )
}
