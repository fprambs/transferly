import { useQuery } from '@tanstack/react-query'

import type { VaultStatus } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

/** Query key of the vault state. */
export const vaultKey = ['vault'] as const

/** Returns the state of the vault, or `undefined` while it loads. */
export function useVaultStatus(): VaultStatus | undefined {
  const api = useApi()
  const { data } = useQuery({ queryKey: vaultKey, queryFn: () => api.vault.status() })
  return data
}
