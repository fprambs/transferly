import { Lock, LockOpen } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Tip } from '@/components/ui/tip'
import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'

import { useVaultStatus, vaultKey } from './use-vault'
import { VaultDialog, type VaultMode } from './VaultDialog'

/**
 * The vault in the top bar: while it is unlocked, a quiet icon that locks it, with its state as its name and tooltip; while it is locked or
 * not set up, a warning pill with the state written out, that unlocks it or sets it up.
 */
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

  const act = async (): Promise<void> => {
    if (unlocked) {
      await api.vault.lock()
      await queryClient.invalidateQueries({ queryKey: vaultKey })
    } else {
      setMode(status.initialized ? 'unlock' : 'setup')
    }
  }

  return (
    <>
      {unlocked ? (
        <Tip label={`${label} · ${t(($) => $.chip.lockAction)}`}>
          <button
            type="button"
            aria-label={label}
            onClick={() => void act()}
            className="flex size-7 cursor-pointer items-center justify-center rounded-full text-ok hover:bg-accent"
          >
            <Icon aria-hidden="true" className="size-4" />
          </button>
        </Tip>
      ) : (
        <button
          type="button"
          onClick={() => void act()}
          className={cn('flex h-7 cursor-pointer items-center gap-2 rounded-full border border-warn/40 bg-warn/15 px-2.5 text-[13px] font-medium text-foreground hover:brightness-110')}
        >
          <Icon aria-hidden="true" className="size-3.5 text-warn" />
          {label}
        </button>
      )}
      <VaultDialog mode={mode} onClose={() => setMode(null)} />
    </>
  )
}
