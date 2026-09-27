import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the administration screens explain. */
const adminErrorKeys = [
  'forbidden',
  'own_account',
  'last_admin',
  'account_not_found',
  'invalid_role',
  'invalid_network',
  'too_many_networks',
  'invalid_limits',
  'rate_limited',
  'network_error',
] as const

/** Translation key of the message that explains a failure. */
export type AdminErrorKey = (typeof adminErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function adminErrorKeyOf(error: unknown): AdminErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return adminErrorKeys.find((key) => key === code) ?? 'unknown'
}
