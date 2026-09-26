import { useVirtualizer } from '@tanstack/react-virtual'
import { File, FileQuestion, Folder, Link2 } from 'lucide-react'
import { useRef, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from '@/components/ui/context-menu'
import type { RemoteEntry } from '@/lib/api'
import { formatInstant } from '@/lib/datetime'
import { cn } from '@/lib/utils'

import { formatSize, resolveSelection } from './entries'

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
  /** Whether the server reports permissions; FTP does not, and its entries show a dash. */
  showPermissions: boolean
  /** Path of the selected entry; the first entry is selected when it is `null` or gone. */
  selectedPath: string | null
  /** Called when the selection moves to an entry. */
  onSelect: (entry: RemoteEntry) => void
  /** Called to download a file from the context menu; the item is left out without it. */
  onDownload?: (entry: RemoteEntry) => void
  /** Called to rename an entry: F2 or the context menu; both are left out without it. */
  onRename?: (entry: RemoteEntry) => void
  /** Called to delete an entry: Delete or the context menu; both are left out without it. */
  onDelete?: (entry: RemoteEntry) => void
  /** Called to create a folder from the context menu; the item is left out without it. */
  onNewFolder?: () => void
  /** Called when the user starts dragging a file, to put what it carries in the drag data. */
  onDragEntry: (entry: RemoteEntry, data: DataTransfer) => void
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
export function EntryTable({ entries, locale, timeZone, onOpen, onUp, showPermissions, selectedPath, onSelect, onDownload, onRename, onDelete, onNewFolder, onDragEntry }: EntryTableProps) {
  const { t } = useTranslation('explorer')
  const scroller = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => rowHeight,
    scrollMargin: rowHeight,
    overscan: 8,
  })
  const current = resolveSelection(entries, selectedPath)
  const active = entries[current]
  const menuItems = active.type === 'directory' || onDownload || onRename || onDelete || onNewFolder

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
    if (event.key === 'F2' && onRename) {
      event.preventDefault()
      onRename(active)
      return
    }
    if (event.key === 'Delete' && onDelete) {
      event.preventDefault()
      onDelete(active)
      return
    }
    const next = movedTo(event.key, current, entries.length - 1)
    if (next !== undefined) {
      event.preventDefault()
      onSelect(entries[next])
      virtualizer.scrollToIndex(next)
    }
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger
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
              onClick={() => onSelect(entry)}
              onContextMenu={() => onSelect(entry)}
              onDoubleClick={() => onOpen(entry)}
              draggable={entry.type === 'file'}
              onDragStart={(event) => onDragEntry(entry, event.dataTransfer)}
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
                {showPermissions ? entry.mode : '—'}
              </span>
            </div>
          )
        })}
      </div>
      </ContextMenuTrigger>
      {menuItems ? (
        <ContextMenuContent aria-label={t(($) => $.menu.label)} className="min-w-44">
          {active.type === 'directory' && <ContextMenuItem onClick={() => onOpen(active)}>{t(($) => $.menu.open)}</ContextMenuItem>}
          {active.type === 'file' && onDownload && <ContextMenuItem onClick={() => onDownload(active)}>{t(($) => $.menu.download)}</ContextMenuItem>}
          {onRename && <ContextMenuItem onClick={() => onRename(active)}>{t(($) => $.menu.rename)}</ContextMenuItem>}
          {onDelete && <ContextMenuItem onClick={() => onDelete(active)}>{t(($) => $.menu.delete)}</ContextMenuItem>}
          {onNewFolder && <ContextMenuSeparator />}
          {onNewFolder && <ContextMenuItem onClick={onNewFolder}>{t(($) => $.menu.newFolder)}</ContextMenuItem>}
        </ContextMenuContent>
      ) : null}
    </ContextMenu>
  )
}