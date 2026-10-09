import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the remote editor explains. */
const editorErrorKeys = ['path_not_found', 'remote_permission_denied', 'remote_unavailable', 'ssh_algorithm_incompatible', 'remote_connection_timeout', 'vault_locked', 'destination_blocked', 'network_error', 'safe_replace_unsupported'] as const

/** Translation key of the message that explains a failure. */
export type EditorErrorKey = (typeof editorErrorKeys)[number] | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function editorErrorKeyOf(error: unknown): EditorErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return editorErrorKeys.find((key) => key === code) ?? 'unknown'
}
