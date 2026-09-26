import { Lock } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { useVaultStatus } from './use-vault'
import { VaultDialog } from './VaultDialog'

/** What the user was doing when the vault turned out to be locked, which decides the wording. */
export type VaultNeed = 'save' | 'connect' | 'team'

/** Props of {@link VaultLockedNotice}. */
export interface VaultLockedNoticeProps {
  /** What needs the vault. */
  need: VaultNeed
  /** Called once the vault is unlocked from the notice, so the caller can try again. */
  onUnlocked?: () => void
}

/**
 * A friendly panel that says the vault is needed for what the user is doing, why, and unlocks it in place: the button opens the
 * master password dialog, or the setup one when there is no vault yet. It replaces a bare error message.
 */
export function VaultLockedNotice({ need, onUnlocked }: VaultLockedNoticeProps) {
  const { t } = useTranslation('vault')
  const status = useVaultStatus()
  const [open, setOpen] = useState(false)
  const initialized = status?.initialized ?? true
  const variant = initialized ? 'locked' : 'missing'

  return (
    <div role="status" className="flex items-start gap-3 rounded-lg border border-warn/40 bg-warn/10 px-3.5 py-3 text-sm">
      <Lock aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warn" />
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="flex flex-col gap-0.5">
          <p className="font-medium">{t(($) => $.notice[variant].title)}</p>
          <p className="leading-relaxed text-muted-foreground">{t(($) => $.notice[variant][need])}</p>
        </div>
        <Button type="button" size="sm" className="self-start" onClick={() => setOpen(true)}>
          {t(($) => $.notice[variant].action)}
        </Button>
      </div>
      <VaultDialog mode={open ? (initialized ? 'unlock' : 'setup') : null} onClose={() => setOpen(false)} onUnlocked={onUnlocked} />
    </div>
  )
}
