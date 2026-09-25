import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the vault dialog explains. */
const vaultErrorKeys = ['invalid_secret', 'weak_password', 'vault_exists', 'vault_not_initialized', 'rate_limited', 'network_error'] as const

/** Translation key of the message that explains a failure. */
export type VaultErrorKey = (typeof vaultErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function vaultErrorKeyOf(error: unknown): VaultErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return vaultErrorKeys.find((key) => key === code) ?? 'unknown'
}
