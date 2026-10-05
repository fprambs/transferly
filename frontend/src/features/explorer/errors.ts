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
  'credentials_required',
  'site_not_found',
  'connector_disabled',
  'quick_connection_not_found',
  'tree_too_large',
  'time_zone_inconclusive',
  'server_commands_unsupported',
  'sudo_refused',
  'remote_quota_exceeded',
  'network_error',
] as const

/** Translation key of the message that explains a failure. */
export type ExplorerErrorKey = (typeof explorerErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function explorerErrorKeyOf(error: unknown): ExplorerErrorKey {
  return explorerErrorKeyOfCode(error instanceof ApiError ? error.code : undefined)
}

/** Maps the stable code of a failure, as the connection log tells it, to the translation key of its message. */
export function explorerErrorKeyOfCode(code: string | undefined): ExplorerErrorKey {
  return explorerErrorKeys.find((key) => key === code) ?? 'unknown'
}
