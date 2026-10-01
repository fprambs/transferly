import { useTranslation } from 'react-i18next'

import { BrandMark } from '@/components/brand/BrandMark'
import { Skeleton } from '@/components/ui/skeleton'

/** Widths of the name placeholders, so the rows do not look stamped. */
const nameWidths = ['w-40', 'w-56', 'w-32', 'w-48', 'w-64', 'w-36', 'w-52', 'w-44']

/** Placeholder rows shaped like the table of entries. */
function SkeletonRows({ count }: { count: number }) {
  return (
    <div aria-hidden="true" className="flex flex-col">
      {Array.from({ length: count }, (_, index) => (
        <div key={index} className="grid h-8 grid-cols-[minmax(10rem,1fr)_88px_160px_112px] items-center gap-3 px-4">
          <span className="flex items-center gap-2">
            <Skeleton className="size-4 rounded" />
            <Skeleton className={`h-3 ${nameWidths[index % nameWidths.length]}`} />
          </span>
          <Skeleton className="ms-auto h-3 w-12" />
          <Skeleton className="h-3 w-28" />
          <Skeleton className="h-3 w-20" />
        </div>
      ))}
    </div>
  )
}

/** Props of {@link ConnectingState}. */
export interface ConnectingStateProps {
  /** Name of the site or host being opened. */
  name: string
}

/** What the explorer shows while it opens a connection and reads its first folder: the mark drawing itself, a message and placeholder rows. */
export function ConnectingState({ name }: ConnectingStateProps) {
  const { t } = useTranslation('explorer')

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div role="status" className="flex items-center gap-3 px-4 py-3">
        <BrandMark animated className="size-8 shrink-0" />
        <div className="flex flex-col">
          <span className="text-sm font-medium">{t(($) => $.connecting.title, { name })}</span>
          <span className="text-xs text-muted-foreground">{t(($) => $.connecting.hint)}</span>
        </div>
      </div>
      <SkeletonRows count={8} />
    </div>
  )
}
