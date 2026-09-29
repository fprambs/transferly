import catalog from '@/i18n/locales/en/errors.json'
import { ApiError } from '@/lib/transport'

/** A stable code of the server that has guidance: a probable cause and a next step (4.16). */
export type GuidedCode = keyof typeof catalog.codes

/** Tells whether a code has guidance. */
function isGuided(code: string): code is GuidedCode {
  return Object.hasOwn(catalog.codes, code)
}

/**
 * Returns the code whose guidance explains a failure, or `undefined` when none does. It takes a failed request or a code; a code the
 * interface derived for the jump host, such as `jump_wrong_passphrase`, is explained as the code of the site's own key.
 */
export function guidedCodeOf(failure: unknown): GuidedCode | undefined {
  const code = failure instanceof ApiError ? failure.code : typeof failure === 'string' ? failure : undefined
  if (code === undefined) {
    return undefined
  }
  const own = code.replace(/^jump_/, '')
  return isGuided(code) ? code : isGuided(own) ? own : undefined
}
