import { ApiError } from '@/lib/transport'

/** Error messages the auth form can show, keyed by their translation. */
export type ErrorKey = 'invalid_credentials' | 'rate_limited' | 'setup_complete' | 'invalid_challenge' | 'network_error' | 'unknown'

/** Translation keys of the errors a failed request maps to. */
const knownErrors: readonly ErrorKey[] = ['invalid_credentials', 'rate_limited', 'setup_complete', 'invalid_challenge', 'network_error']

/** Maps a failed request to the translation key of its message. */
export function errorKeyOf(error: unknown): ErrorKey {
  if (error instanceof ApiError) {
    return knownErrors.find((key) => key === error.code) ?? 'unknown'
  }
  return 'unknown'
}
