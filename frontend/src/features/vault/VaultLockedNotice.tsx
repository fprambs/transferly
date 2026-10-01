import { KeyRound, Lock, LockKeyhole, Zap } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'

import { useVaultStatus } from './use-vault'
import { VaultDialog, type VaultTarget } from './VaultDialog'

/** What the user was doing when the vault turned out to be locked, which decides the wording. */
export type VaultNeed = 'save' | 'connect' | 'team' | 'export'

/** Props of {@link VaultLockedNotice}. */
export interface VaultLockedNoticeProps {
  /** What needs the vault. */
  need: VaultNeed
  /** Called once the vault is unlocked from the notice, so the caller can try again. */
  onUnlocked?: () => void
  /** Whether the master password dialog opens at once, as FileZilla asks for its master password, instead of waiting for the button. */
  autoOpen?: boolean
  /** The server that needs the vault, named in the dialog. */
  target?: VaultTarget
  /** `inline` fits inside a form; `page` is a card centered in an otherwise empty screen, for when the vault is all that stands in the way. */
  layout?: 'inline' | 'page'
}

/**
 * A friendly panel that says the vault is needed for what the user is doing, why, and unlocks it in place: the button opens the
 * master password dialog, or the setup one when there is no vault yet; with `autoOpen` the dialog opens at once and the panel stays behind in case
 * it is dismissed. It replaces a bare error message.
 */
export function VaultLockedNotice({ need, onUnlocked, autoOpen = false, target, layout = 'inline' }: VaultLockedNoticeProps) {
  const { t } = useTranslation('vault')
  const status = useVaultStatus()
  const [open, setOpen] = useState(autoOpen)
  const initialized = status?.initialized ?? true
  const variant = initialized ? 'locked' : 'missing'
  const dialog = <VaultDialog mode={open ? (initialized ? 'unlock' : 'setup') : null} onClose={() => setOpen(false)} onUnlocked={onUnlocked} target={target} />

  if (layout === 'page') {
    return (
      <>
        <PanelState icon={LockKeyhole} tone="warn" title={t(($) => $.notice[variant].title)} description={t(($) => $.notice[variant][need])}>
          {target !== undefined && (
            <div className="flex max-w-full flex-col items-center gap-0.5 rounded-lg border bg-muted/40 px-4 py-2">
              <span className="max-w-full truncate text-sm font-medium">{target.name}</span>
              <span className="max-w-full truncate font-mono text-xs text-muted-foreground">
                {target.username}@{target.host}
              </span>
            </div>
          )}
          <Button type="button" size="lg" className="min-w-52" onClick={() => setOpen(true)}>
            <KeyRound />
            {t(($) => $.notice[variant].action)}
          </Button>
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Zap aria-hidden="true" className="size-3.5 text-warn" />
            {t(($) => $.notice.quickHint)}
          </p>
        </PanelState>
        {dialog}
      </>
    )
  }
  return (
    <div role="status" className="flex items-start gap-3 rounded-lg border border-warn/40 bg-warn/10 px-3.5 py-3 text-sm duration-200 animate-in fade-in-0 slide-in-from-bottom-1">
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
      {dialog}
    </div>
  )
}
