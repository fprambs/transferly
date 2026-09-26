import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the explorer explains. */
const explorerErrorKeys = [
  'host_key_changed',
  'remote_auth_failed',
  'remote_unavailable',
  'remote_permission_denied',
  'path_not_found',
  'not_a_directory',
  'path_exists',
  'directory_not_empty',
  'destination_blocked',
  'host_unresolvable',
  'vault_locked',
  'site_not_found',
  'quick_connection_not_found',
  'tree_too_large',
  'time_zone_inconclusive',
  'network_error',
] as const

/** Translation key of the message that explains a failure. */
export type ExplorerErrorKey = (typeof explorerErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function explorerErrorKeyOf(error: unknown): ExplorerErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return explorerErrorKeys.find((key) => key === code) ?? 'unknown'
}
