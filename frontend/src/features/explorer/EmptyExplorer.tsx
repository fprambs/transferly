import { useTranslation } from 'react-i18next'

import { Illustration } from '@/components/brand/Illustration'

/** What the explorer area shows before any site is opened. */
export function EmptyExplorer() {
  const { t } = useTranslation('explorer')

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
      <Illustration scene="start" className="mb-2 h-28 w-42" />
      <h2 className="text-base font-medium">{t(($) => $.empty.title)}</h2>
      <p className="max-w-sm text-sm text-muted-foreground">{t(($) => $.empty.body)}</p>
    </div>
  )
}
