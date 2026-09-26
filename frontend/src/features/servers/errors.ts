import { ApiError } from '@/lib/transport'

/** Translation keys of the failures of forgetting a server. */
export type ForgetError = 'forbidden' | 'host_key_not_found' | 'network_error' | 'unknown'

/** Codes that have a message of their own. */
const knownCodes: readonly string[] = ['forbidden', 'host_key_not_found', 'network_error']

/** Maps a failed request to the translation key of its message. */
export function forgetErrorOf(error: unknown): ForgetError {
  const code = error instanceof ApiError ? error.code : undefined
  return knownCodes.includes(code as string) ? (code as ForgetError) : 'unknown'
}
