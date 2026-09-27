import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the site and collection dialogs explain. */
const siteErrorKeys = [
  'collection_name_taken',
  'collection_cycle',
  'collection_too_deep',
  'collection_not_empty',
  'collection_not_found',
  'forbidden',
  'site_name_taken',
  'invalid_name',
  'invalid_host',
  'invalid_port',
  'invalid_username',
  'invalid_timezone',
  'invalid_charset',
  'invalid_speed_limit',
  'invalid_proxy',
  'invalid_jump',
  'invalid_protocol',
  'invalid_auth_method',
  'secret_too_large',
  'vault_locked',
  'host_key_changed',
  'remote_auth_failed',
  'remote_unavailable',
  'destination_blocked',
  'host_unresolvable',
  'site_not_found',
  'invalid_import_file',
  'import_file_too_large',
  'network_error',
] as const

/** Translation key of the message that explains a failure. */
export type SiteErrorKey = (typeof siteErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function siteErrorKeyOf(error: unknown): SiteErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return siteErrorKeys.find((key) => key === code) ?? 'unknown'
}
