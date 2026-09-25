import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the quick connect and save dialogs explain. */
export const quickErrorKeys = [
  'host_key_changed',
  'remote_auth_failed',
  'remote_unavailable',
  'destination_blocked',
  'host_unresolvable',
  'invalid_host',
  'invalid_port',
  'invalid_protocol',
  'rate_limited',
  'too_many_quick_connections',
  'site_name_taken',
  'vault_locked',
  'network_error',
] as const

/** Translation key of the message that explains a failure. */
export type QuickErrorKey = (typeof quickErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function quickErrorKeyOf(error: unknown): QuickErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return quickErrorKeys.find((key) => key === code) ?? 'unknown'
}

/** What the server reported about a key it does not know yet. */
export interface UnknownHostKey {
  /** Algorithm of the key. */
  keyType: string
  /** Fingerprint of the key. */
  fingerprint: string
}

/** Extracts the key of a `host_key_unknown` failure, or `undefined` for any other error. */
export function unknownHostKeyOf(error: unknown): UnknownHostKey | undefined {
  if (!(error instanceof ApiError) || error.code !== 'host_key_unknown') {
    return undefined
  }
  const { keyType, fingerprint } = error.problem.params ?? {}
  return typeof keyType === 'string' && typeof fingerprint === 'string' ? { keyType, fingerprint } : undefined
}
