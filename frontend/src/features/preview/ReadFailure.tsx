import { CircleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'
import { ApiError } from '@/lib/transport'

import { ErrorGuide } from '../errors/ErrorGuide'
import { VaultLockedNotice } from '../vault/VaultLockedNotice'

/** Props of {@link ReadFailure}. */
export interface ReadFailureProps {
  /** Why the read failed. */
  error: unknown
  /** What could not be done, as the title of the panel. */
  title: string
  /** Reads again; called once the vault is unlocked too. */
  onRetry?: () => void
}

/**
 * Says why a read of the viewer failed, centered. A locked vault is not a dead end: the master password dialog opens at once, and the
 * read is tried again once the vault is unlocked. Other failures show the guidance of their code and can be tried again.
 */
export function ReadFailure({ error, title, onRetry }: ReadFailureProps) {
  const { t } = useTranslation('preview')
  if (error instanceof ApiError && error.code === 'vault_locked') {
    return <VaultLockedNotice need="connect" layout="page" autoOpen onUnlocked={onRetry} />
  }
  return (
    <PanelState icon={CircleAlert} tone="danger" role="alert" title={title}>
      <ErrorGuide failure={error} />
      {onRetry !== undefined && (
        <Button variant="outline" onClick={onRetry}>
          {t(($) => $.retry)}
        </Button>
      )}
    </PanelState>
  )
}
