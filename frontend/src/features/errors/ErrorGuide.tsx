import { useTranslation } from 'react-i18next'

import { ApiError } from '@/lib/transport'
import { cn } from '@/lib/utils'

import { guidedCodeOf } from './guide'

/** Props of {@link ErrorGuide}. */
export interface ErrorGuideProps {
  /** The failure to explain: a failed request or the stable code of the server. */
  failure: unknown
  /** Extra classes of the list. */
  className?: string
}

/**
 * Explains a failure beyond its message (4.16): the probable cause and what to do next, and the stable code to quote when asking for
 * help. It shows nothing for a failure without guidance.
 */
export function ErrorGuide({ failure, className }: ErrorGuideProps) {
  const { t } = useTranslation('errors')
  const code = guidedCodeOf(failure)
  if (code === undefined) {
    return null
  }
  const params = failure instanceof ApiError ? failure.problem.params : undefined
  const category = typeof params?.category === 'string' && ['key_exchange', 'cipher', 'mac', 'host_signature', 'authentication_signature'].includes(params.category)
    ? params.category as 'key_exchange' | 'cipher' | 'mac' | 'host_signature' | 'authentication_signature'
    : 'unknown'
  const explanation = code === 'ssh_algorithm_incompatible'
    ? { category: t(($) => $.sshCategories[category]), serverAlgorithms: params?.serverAlgorithms ?? t(($) => $.sshUnknownAlgorithms) }
    : params
  return (
    <dl className={cn('grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-start text-xs duration-200 animate-in fade-in-0', className)}>
      <dt className="font-medium">{t(($) => $.guide.cause)}</dt>
      <dd>{t(($) => $.codes[code].cause, explanation)}</dd>
      <dt className="font-medium">{t(($) => $.guide.next)}</dt>
      <dd>{t(($) => $.codes[code].next)}</dd>
      <dd className="col-span-2 font-mono opacity-70">{t(($) => $.guide.code, { code })}</dd>
    </dl>
  )
}
