import { ApiError } from '@/lib/transport'

/** Translation keys of the failures a sync run explains, reusing the codes the file endpoints already answer with. */
const syncErrorKeys = [
  'host_key_changed',
  'remote_auth_failed',
  'remote_unavailable',
  'remote_permission_denied',
  'path_not_found',
  'path_exists',
  'destination_blocked',
  'host_unresolvable',
  'vault_locked',
  'site_not_found',
  'daily_quota_reached',
  'network_error',
] as const

/** Translation key of the message that explains a failure; a local file system failure gets its own generic key. */
export type SyncErrorKey = (typeof syncErrorKeys)[number] | 'localFailure' | 'tooMany' | 'unknown'

/** Maps a failed row of a sync run to the translation key of its message. */
export function syncErrorKeyOf(error: unknown): SyncErrorKey {
  if (!(error instanceof ApiError)) {
    return 'localFailure'
  }
  return syncErrorKeys.find((key) => key === error.code) ?? 'unknown'
}
