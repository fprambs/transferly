import { useTranslation } from 'react-i18next'

import type { AdminErrorKey } from './errors'

/** Props of {@link AdminFailure}. */
export interface AdminFailureProps {
  /** The failure to explain, or `null` for none. */
  failure: AdminErrorKey | null
}

/** An alert that explains why an administration action failed; nothing is drawn while there is no failure. */
export function AdminFailure({ failure }: AdminFailureProps) {
  const { t } = useTranslation('admin')
  if (failure === null) {
    return null
  }
  return (
    <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {t(($) => $.errors[failure])}
    </p>
  )
}
