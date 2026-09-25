import { useVirtualizer } from '@tanstack/react-virtual'
import { File, FileQuestion, Folder, Link2 } from 'lucide-react'
import { useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { RemoteEntry } from '@/lib/api'
import { formatInstant } from '@/lib/datetime'
import { cn } from '@/lib/utils'

import { formatSize } from './entries'

/** Height of every row, in pixels. */
const rowHeight = 32

/** Column layout shared by the header and the rows. */
const columns = 'grid grid-cols-[minmax(0,1fr)_88px_160px_112px] items-center gap-3 px-4'

/** Props of {@link EntryTable}. */
export interface EntryTableProps {
  /** Entries to list, already sorted. */
  entries: readonly RemoteEntry[]
  /** Locale used to format dates. */
  locale: string
  /** IANA time zone dates are shown in. */
  timeZone: string
  /** Called when the user opens an entry: Enter or double click. */
  onOpen: (entry: RemoteEntry) => void
  /** Called when the user asks to go up one level with Backspace. */
  onUp: () => void
}

/** Icon that tells the kind of an entry. */
function EntryIcon({ type }: { type: RemoteEntry['type'] }) {
  const className = 'size-4 shrink-0'
  if (type === 'directory') {
    return <Folder aria-hidden="true" className={cn(className, 'text-accent-text')} />
  }
  if (type === 'symlink') {
    return <Link2 aria-hidden="true" className={cn(className, 'text-info')} />
  }
  return type === 'file' ? (
    <File aria-hidden="true" className={cn(className, 'text-muted-foreground')} />
  ) : (
    <FileQuestion aria-hidden="true" className={cn(className, 'text-muted-foreground')} />
  )
}

/** Formats the modification time of an entry, leaving out the time when the server only gave the day. */
function modified(entry: RemoteEntry, locale: string, timeZone: string): string {
  return formatInstant(entry.modifiedAt, {
    locale,
    timeZone,
    dateStyle: 'medium',
    timeStyle: entry.modifiedPrecision === 'day' ? undefined : 'short',
  })
}

/** Moves the selection for a navigation key, or returns `undefined` when the key is not one. */
function movedTo(key: string, current: number, last: number): number | undefined {
  const moves: Record<string, number> = {
    ArrowDown: Math.min(current + 1, last),
    ArrowUp: Math.max(current - 1, 0),
    Home: 0,
    End: last,
  }
  return moves[key]
}

/** Virtualized table of the entries of a folder, with keyboard navigation. It needs at least one entry. */
export function EntryTable({ entries, locale, timeZone, onOpen, onUp }: EntryTableProps) {
  const { t } = useTranslation('explorer')
  const scroller = useRef<HTMLDivElement>(null)
  const [selected, setSelected] = useState(0)
  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => rowHeight,
    scrollMargin: rowHeight,
    overscan: 8,
  })
  const current = Math.min(selected, entries.length - 1)
  const active = entries[current]

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    if (event.key === 'Enter') {
      event.preventDefault()
      onOpen(active)
      return
    }
    if (event.key === 'Backspace') {
      event.preventDefault()
      onUp()
      return
    }
    const next = movedTo(event.key, current, entries.length - 1)
    if (next !== undefined) {
      event.preventDefault()
      setSelected(next)
      virtualizer.scrollToIndex(next)
    }
  }

  return (
    <div
      ref={scroller}
      role="grid"
      aria-label={t(($) => $.list)}
      aria-rowcount={entries.length + 1}
      tabIndex={0}
      aria-activedescendant={`entry-${current}`}
      onKeyDown={onKeyDown}
      className="min-h-0 flex-1 overflow-auto outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      <div
        role="row"
        className={cn(columns, 'sticky top-0 z-10 h-8 border-b bg-background text-[11px] font-medium tracking-wider text-muted-foreground uppercase')}
      >
        <span role="columnheader">{t(($) => $.columns.name)}</span>
        <span role="columnheader" className="text-end">
          {t(($) => $.columns.size)}
        </span>
        <span role="columnheader">{t(($) => $.columns.modified)}</span>
        <span role="columnheader">{t(($) => $.columns.mode)}</span>
      </div>
      <div role="rowgroup" style={{ height: virtualizer.getTotalSize() }} className="relative w-full">
        {virtualizer.getVirtualItems().map((item) => {
          const entry = entries[item.index]
          return (
            <div
              key={entry.path}
              id={`entry-${item.index}`}
              role="row"
              aria-rowindex={item.index + 2}
              aria-selected={item.index === current}
              onClick={() => setSelected(item.index)}
              onDoubleClick={() => onOpen(entry)}
              style={{ height: rowHeight, transform: `translateY(${item.start - rowHeight}px)` }}
              className={cn(
                columns,
                'absolute start-0 top-0 w-full cursor-pointer text-[13px] hover:bg-accent hover:**:text-foreground aria-selected:bg-primary/15 aria-selected:**:text-foreground',
              )}
            >
              <span role="gridcell" className="flex min-w-0 items-center gap-2">
                <EntryIcon type={entry.type} />
                <span className="truncate">{entry.name}</span>
              </span>
              <span role="gridcell" className="text-end font-mono text-xs text-muted-foreground">
                {entry.type === 'directory' ? '—' : formatSize(entry.size)}
              </span>
              <span role="gridcell" className="truncate text-xs text-muted-foreground">
                {modified(entry, locale, timeZone)}
              </span>
              <span role="gridcell" className="font-mono text-xs text-muted-foreground">
                {entry.mode}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
