import { ShieldQuestion } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

import type { UnknownHostKey } from './errors'

/** Props of {@link TrustPanel}. */
export interface TrustPanelProps {
  /** Host the key belongs to. */
  host: string
  /** The key the server presented. */
  hostKey: UnknownHostKey
  /** Whether trusting is in progress. */
  busy: boolean
  /** Called when the user goes back without trusting. */
  onBack: () => void
  /** Called when the user trusts the key. */
  onTrust: () => void
}

/** Asks the user to verify the fingerprint of a server seen for the first time. */
export function TrustPanel({ host, hostKey, busy, onBack, onTrust }: TrustPanelProps) {
  const { t } = useTranslation('quick')

  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          <ShieldQuestion className="size-5 text-warn" />
          {t(($) => $.trust.title)}
        </DialogTitle>
        <DialogDescription>{t(($) => $.trust.description, { host })}</DialogDescription>
      </DialogHeader>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 rounded-lg border bg-muted/40 p-3 text-sm">
        <dt className="text-muted-foreground">{t(($) => $.trust.keyType)}</dt>
        <dd className="font-mono">{hostKey.keyType}</dd>
        <dt className="text-muted-foreground">{t(($) => $.trust.fingerprint)}</dt>
        <dd className="font-mono break-all">{hostKey.fingerprint}</dd>
      </dl>
      <DialogFooter>
        <Button variant="outline" onClick={onBack} disabled={busy}>
          {t(($) => $.trust.back)}
        </Button>
        <Button onClick={onTrust} disabled={busy}>
          {t(($) => $.trust.confirm)}
        </Button>
      </DialogFooter>
    </>
  )
}
