import { useTranslation } from 'react-i18next'

import type { TeamErrorKey } from './errors'

/** Props of {@link TeamFailure}. */
export interface TeamFailureProps {
  /** The failure to explain, or `null` for none. */
  failure: TeamErrorKey | null
}

/** An alert that explains why a team action failed; nothing is drawn while there is no failure. */
export function TeamFailure({ failure }: TeamFailureProps) {
  const { t } = useTranslation('teams')
  if (failure === null) {
    return null
  }
  return (
    <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
      {t(($) => $.errors[failure])}
    </p>
  )
}
