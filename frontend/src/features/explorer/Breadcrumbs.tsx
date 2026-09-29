import { ChevronRight } from 'lucide-react'
import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { pathSegments } from './entries'

/** Props of {@link Breadcrumbs}. */
export interface BreadcrumbsProps {
  /** Absolute path being shown. */
  path: string
  /** Called with the path of the step the user picked. */
  onNavigate: (path: string) => void
}

/** Path of the folder in view as clickable steps; the last one is the current folder. */
export function Breadcrumbs({ path, onNavigate }: BreadcrumbsProps) {
  const { t } = useTranslation('explorer')
  const segments = pathSegments(path)

  return (
    <nav aria-label={t(($) => $.breadcrumbs)} className="min-w-20 flex-1 overflow-hidden">
      <ol className="flex flex-nowrap items-center gap-1 font-mono text-[13px] whitespace-nowrap">
        {segments.map((segment, index) => {
          const current = index === segments.length - 1
          return (
            <Fragment key={segment.path}>
              {index > 1 && <ChevronRight aria-hidden="true" className="size-3 shrink-0 text-faint rtl:-scale-x-100" />}
              <li className="min-w-0">
                <button
                  type="button"
                  aria-current={current ? 'page' : undefined}
                  onClick={() => onNavigate(segment.path)}
                  className={cn(
                    'max-w-48 cursor-pointer truncate rounded px-1.5 py-0.5 hover:bg-accent',
                    current ? 'font-medium text-foreground' : 'text-muted-foreground',
                  )}
                >
                  {segment.name}
                </button>
              </li>
            </Fragment>
          )
        })}
      </ol>
    </nav>
  )
}
