import type { TFunction } from 'i18next'

import { explorerErrorKeyOfCode } from '../explorer/errors'

import type { LogLine } from './connection-log-store'

/** The key type the server uses for the certificate of an FTPS server. */
const certificateKeyType = 'tls-certificate'

/** Steps whose message the log knows; any other is shown by its code. */
const knownSteps = ['connecting', 'connected', 'host_key_verified', 'host_key_unknown', 'host_key_changed', 'plain_text', 'authenticated', 'listing', 'listed', 'failed', 'canceled', 'disconnected'] as const

/** Logon types the log names; any other is shown as it came. */
const knownMethods = ['password', 'key', 'ask', 'anonymous'] as const

/** Writes the message of one line in the language of the interface. */
export function messageOf(line: LogLine, t: TFunction<'explorer'>): string {
  const params = line.params
  const step = knownSteps.find((known) => known === line.step)
  let text: string
  switch (step) {
    case undefined:
      text = line.step
      break
    case 'connecting':
      text = t(($) => $.log.steps.connecting[params.via === 'proxy' ? 'proxy' : params.via === 'jump' ? 'jump' : 'direct'], params)
      break
    case 'host_key_verified':
    case 'host_key_unknown':
    case 'host_key_changed':
      text = t(($) => $.log.steps[step][params.keyType === certificateKeyType ? 'certificate' : 'key'], params)
      break
    case 'authenticated': {
      const method = knownMethods.find((known) => known === params.method)
      text = t(($) => $.log.steps.authenticated, { ...params, method: method === undefined ? params.method : t(($) => $.log.methods[method]) })
      break
    }
    case 'failed':
      text = t(($) => $.log.steps.failed, { ...params, reason: t(($) => $.errors[explorerErrorKeyOfCode(params.code)]) })
      break
    default:
      text = t(($) => $.log.steps[step], params)
  }
  return params.hop === 'jump' ? t(($) => $.log.jump, { text }) : text
}
