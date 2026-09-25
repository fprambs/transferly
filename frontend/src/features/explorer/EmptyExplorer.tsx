import { FolderOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

/** What the explorer area shows before any site is opened. */
export function EmptyExplorer() {
  const { t } = useTranslation('explorer')

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center">
      <FolderOpen aria-hidden="true" className="size-10 text-faint" />
      <h2 className="text-base font-medium">{t(($) => $.empty.title)}</h2>
      <p className="max-w-sm text-sm text-muted-foreground">{t(($) => $.empty.body)}</p>
    </div>
  )
}
