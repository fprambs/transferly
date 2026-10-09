import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the site and collection dialogs explain. */
const siteErrorKeys = [
  'collection_name_taken',
  'collection_cycle',
  'collection_too_deep',
  'collection_not_empty',
  'collection_not_found',
  'forbidden',
  'elevation_forbidden',
  'site_name_taken',
  'invalid_name',
  'invalid_host',
  'invalid_port',
  'invalid_username',
  'invalid_timezone',
  'invalid_charset',
  'invalid_speed_limit',
  'invalid_remote_dir',
  'invalid_color',
  'invalid_proxy',
  'invalid_jump',
  'invalid_protocol',
  'connector_disabled',
  'invalid_auth_method',
  'secret_too_large',
  'vault_locked',
  'credentials_required',
  'credentials_not_asked',
  'host_key_changed',
  'remote_auth_failed',
  'remote_unavailable',
  'ssh_algorithm_incompatible', 'remote_connection_timeout',
  'destination_blocked',
  'host_unresolvable',
  'site_not_found',
  'invalid_import_file',
  'import_file_too_large',
  'invalid_export',
  'wrong_export_password',
  'weak_export_password',
  'network_error',
  'private_key_is_public',
  'passphrase_required',
  'wrong_passphrase',
  'private_key_unreadable',
  'private_key_unsupported',
  'jump_private_key_is_public',
  'jump_passphrase_required',
  'jump_wrong_passphrase',
  'jump_private_key_unreadable',
  'jump_private_key_unsupported',
] as const

/** Translation key of the message that explains a failure. */
export type SiteErrorKey = (typeof siteErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message; a private key the server refused is told apart as the
 * site's or the jump host's by the field that holds it. */
export function siteErrorKeyOf(error: unknown): SiteErrorKey {
  const code = error instanceof ApiError ? (error.problem.params?.field === 'jump_private_key' ? `jump_${error.code}` : error.code) : undefined
  return siteErrorKeys.find((key) => key === code) ?? 'unknown'
}

/** The field a refused request names, such as the private key the server could not use, if it names one. */
export function fieldOf(error: unknown): string | undefined {
  return error instanceof ApiError ? (error.problem.params?.field as string | undefined) : undefined
}

/** The setting of a connector the server refused as invalid_config and why, with the reason the schema gives, if that is the failure. */
export function configFailureOf(error: unknown): { field: string; reason: string } | undefined {
  const field = fieldOf(error)
  return error instanceof ApiError && error.code === 'invalid_config' && field !== undefined ? { field, reason: String(error.problem.params?.reason) } : undefined
}
