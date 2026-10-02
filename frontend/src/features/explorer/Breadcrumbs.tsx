import { ChevronRight, PencilLine } from 'lucide-react'
import { Fragment, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

import { normalizePath, pathSegments } from './entries'

/** Props of {@link Breadcrumbs}. */
export interface BreadcrumbsProps {
  /** Absolute path being shown. */
  path: string
  /** Called with the path of the step the user picked. */
  onNavigate: (path: string) => void
}

/**
 * Path of the folder in view as clickable steps; the last one is the current folder. Its pencil, or a double click beside the steps, turns
 * it into a field to type a path: Enter goes there, Escape or leaving the field puts the steps back.
 */
export function Breadcrumbs({ path, onNavigate }: BreadcrumbsProps) {
  const { t } = useTranslation('explorer')
  const segments = pathSegments(path)
  const [editing, setEditing] = useState(false)

  if (editing) {
    return (
      <form
        className="max-w-[60%] min-w-48 flex-1"
        onSubmit={(event) => {
          event.preventDefault()
          const field = new FormData(event.currentTarget).get('path')
          setEditing(false)
          onNavigate(normalizePath(String(field)))
        }}
      >
        <Input
          name="path"
          aria-label={t(($) => $.pathField)}
          defaultValue={path}
          autoFocus
          spellCheck={false}
          autoComplete="off"
          onFocus={(event) => event.currentTarget.select()}
          onBlur={() => setEditing(false)}
          onKeyDown={(event) => event.key === 'Escape' && (event.stopPropagation(), setEditing(false))}
          className="h-7 font-mono text-[13px]"
        />
      </form>
    )
  }
  return (
    <nav aria-label={t(($) => $.breadcrumbs)} onDoubleClick={(event) => event.target === event.currentTarget && setEditing(true)} className="group/crumbs flex max-w-[60%] min-w-20 shrink items-center gap-1 overflow-hidden">
      <ol className="flex min-w-0 flex-nowrap items-center gap-1 overflow-hidden font-mono text-[13px] whitespace-nowrap">
        {segments.map((segment, index) => {
          const current = index === segments.length - 1
          return (
            <Fragment key={segment.path}>
              {index > 1 && <ChevronRight aria-hidden="true" className="size-3 shrink-0 text-faint rtl:-scale-x-100" />}
              <li className={cn('max-w-48 min-w-0', current && 'shrink-0')}>
                <button
                  type="button"
                  aria-current={current ? 'page' : undefined}
                  onClick={() => onNavigate(segment.path)}
                  className={cn(
                    'block max-w-full cursor-pointer truncate rounded px-1.5 py-0.5 hover:bg-accent',
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
      <button
        type="button"
        aria-label={t(($) => $.pathEdit)}
        onClick={() => setEditing(true)}
        className="flex size-6 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 group-hover/crumbs:opacity-100 hover:bg-accent hover:text-foreground focus-visible:opacity-100"
      >
        <PencilLine aria-hidden="true" className="size-3.5" />
      </button>
    </nav>
  )
}
