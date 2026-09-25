import { Lock, LockOpen } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'

import { useVaultStatus, vaultKey } from './use-vault'
import { VaultDialog, type VaultMode } from './VaultDialog'

/** Pill of the top bar that shows the state of the vault and opens, locks or sets it up. */
export function VaultChip() {
  const { t } = useTranslation('vault')
  const api = useApi()
  const queryClient = useQueryClient()
  const status = useVaultStatus()
  const [mode, setMode] = useState<VaultMode | null>(null)

  if (status === undefined) {
    return null
  }
  const unlocked = status.initialized && status.unlocked
  const label = !status.initialized ? t(($) => $.chip.notSet) : unlocked ? t(($) => $.chip.unlocked) : t(($) => $.chip.locked)
  const Icon = unlocked ? LockOpen : Lock

  return (
    <>
      <button
        type="button"
        title={unlocked ? t(($) => $.chip.lockAction) : undefined}
        onClick={async () => {
          if (unlocked) {
            await api.vault.lock()
            await queryClient.invalidateQueries({ queryKey: vaultKey })
          } else {
            setMode(status.initialized ? 'unlock' : 'setup')
          }
        }}
        className={cn(
          'flex h-7 cursor-pointer items-center gap-2 rounded-full px-2.5 text-[13px] font-medium text-foreground hover:brightness-110',
          unlocked ? 'bg-primary/15' : 'bg-warn/15',
        )}
      >
        <Icon className={cn('size-3.5', unlocked ? 'text-accent-text' : 'text-warn')} />
        {label}
      </button>
      <VaultDialog mode={mode} onClose={() => setMode(null)} />
    </>
  )
}
