import { ApiError } from '@/lib/transport'

/** Translation keys of the failures the team screens explain. */
const teamErrorKeys = [
  'forbidden',
  'invalid_name',
  'invalid_role',
  'invalid_email',
  'member_not_found',
  'owner_bound',
  'personal_workspace',
  'already_member',
  'too_many_invitations',
  'vault_locked',
  'workspace_not_found',
  'invitation_not_found',
  'account_exists',
  'weak_password',
  'invalid_credentials',
  'rate_limited',
  'network_error',
] as const

/** Translation keys of the mistakes the forms catch before asking the server. */
type FormMistake = 'nameRequired' | 'emailRequired' | 'passwordShort' | 'passwordMismatch'

/** Translation key of the message that explains a failure. */
export type TeamErrorKey = (typeof teamErrorKeys)[number] | FormMistake | 'unknown'

/** Maps a failed request to the translation key of its message. */
export function teamErrorKeyOf(error: unknown): TeamErrorKey {
  const code = error instanceof ApiError ? error.code : undefined
  return teamErrorKeys.find((key) => key === code) ?? 'unknown'
}
