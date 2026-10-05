import { ShieldQuestion } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'
import type { HostKeyTrustMode } from '@/lib/api'

import { ChangedHostKeyNotice } from '../explorer/ChangedHostKeyNotice'
import { explorerErrorKeyOf, type ExplorerErrorKey } from '../explorer/errors'
import type { ChangedHostKey, UnknownHostKey } from '../quick/errors'

/** Trusts a key the server of a site presented, next to the trusted ones or instead of them when it says how. */
export type TrustKey = (key: UnknownHostKey, mode?: HostKeyTrustMode) => Promise<void>

/** Props of {@link HostKeyPrompt}. */
export interface HostKeyPromptProps {
  /** The key the server presented, with the trusted ones when it differs from them. */
  hostKey: UnknownHostKey | ChangedHostKey
  /** Trusts the key. */
  trust: TrustKey
  /** Called once the key is trusted, to read again. */
  onTrusted: () => void
}

/**
 * Asks about a server key that a read of the viewer met on a new connection, as the explorer does: a key not seen before is trusted
 * with one click, and a key other than the trusted ones is added next to them, for a service with several servers behind one name, or
 * replaces them.
 */
export function HostKeyPrompt({ hostKey, trust, onTrusted }: HostKeyPromptProps) {
  const { t } = useTranslation('explorer')
  const [failure, setFailure] = useState<ExplorerErrorKey | null>(null)

  const decide = async (key: UnknownHostKey, mode?: HostKeyTrustMode): Promise<void> => {
    try {
      await trust(key, mode)
    } catch (cause) {
      setFailure(explorerErrorKeyOf(cause))
      return
    }
    setFailure(null)
    onTrusted()
  }

  if ('trusted' in hostKey) {
    return <ChangedHostKeyNotice hostKey={hostKey} failure={failure} onTrust={(mode) => decide(hostKey, mode)} />
  }
  return (
    <PanelState icon={ShieldQuestion} tone="warn" role="alert" title={t(($) => $.trust.title)} description={t(($) => $.trust.body)}>
      <dl className="grid w-full grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 rounded-lg border bg-muted/40 px-4 py-2 text-start text-sm">
        <dt className="text-muted-foreground">{t(($) => $.trust.keyType)}</dt>
        <dd className="font-mono">{hostKey.keyType}</dd>
        <dt className="text-muted-foreground">{t(($) => $.trust.fingerprint)}</dt>
        <dd className="font-mono break-all">{hostKey.fingerprint}</dd>
      </dl>
      {failure !== null && <p className="text-sm text-destructive">{t(($) => $.errors[failure])}</p>}
      <Button size="lg" className="min-w-52" onClick={() => void decide(hostKey)}>
        {t(($) => $.trust.confirm)}
      </Button>
    </PanelState>
  )
}
