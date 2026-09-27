import type { MfaChallenge } from '@/lib/api'
import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the second factor screens explain. */
const securityErrorKeys = [
  'invalid_code',
  'invalid_challenge',
  'invalid_password',
  'totp_not_started',
  'totp_not_enabled',
  'user_disabled',
  'rate_limited',
  'invalid_passkey',
  'ceremony_expired',
  'too_many_passkeys',
  'invalid_name',
  'no_passkeys',
  'passkey_not_found',
  'network_error',
] as const

/** Translation keys of the mistakes the forms catch before asking the server. */
type FormMistake = 'codeRequired' | 'passwordRequired' | 'passkeyCancelled' | 'passkeyUnsupported'

/** Translation key of the message that explains a failure. */
export type SecurityErrorKey = (typeof securityErrorKeys)[number] | FormMistake | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function securityErrorKeyOf(error: unknown): SecurityErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return securityErrorKeys.find((key) => key === code) ?? 'unknown'
}

/** Tells whether the answer to a password sign-in asks for a second step instead of opening a session. */
export function needsSecondStep<T extends object>(result: T): result is T & MfaChallenge {
  return 'challenge' in result
}
