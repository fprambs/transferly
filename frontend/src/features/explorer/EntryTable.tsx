import { useVirtualizer } from '@tanstack/react-virtual'
import { Download, Eye, FileArchive, FilePen, Files, FolderOpen, FolderPlus, TextCursorInput, Trash2, type LucideIcon } from 'lucide-react'
import { useRef, type KeyboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ContextMenu, ContextMenuContent, ContextMenuGroup, ContextMenuItem, ContextMenuSeparator, ContextMenuShortcut, ContextMenuTrigger } from '@/components/ui/context-menu'
import type { RemoteEntry } from '@/lib/api'
import { formatInstant } from '@/lib/datetime'
import { cn } from '@/lib/utils'

import { formatSize, resolveSelection } from './entries'
import { matchRanges } from './entry-filter'
import { fileTypeOf } from './file-types'
import { FileTypeIcon } from './FileTypeIcon'
import { useDescribeType } from './use-file-type'

/** Props of {@link MenuEntry}. */
interface MenuEntryProps {
  /** Icon of the action. */
  icon: LucideIcon
  /** Name of the action. */
  label: string
  /** The key that does the same in the list, shown on the right; left out of the name read aloud. */
  shortcut?: string
  /** Whether the action destroys something, which draws it in red. */
  destructive?: boolean
  /** Runs the action. */
  onClick: () => void
}

/** One action of the menu of an entry: its icon, its name and the key that does the same. */
function MenuEntry({ icon: Icon, label, shortcut, destructive = false, onClick }: MenuEntryProps) {
  return (
    <ContextMenuItem variant={destructive ? 'destructive' : 'default'} onClick={onClick} className="gap-2.5">
      <Icon aria-hidden="true" className={destructive ? undefined : 'text-muted-foreground'} />
      {label}
      {shortcut !== undefined && (
        <ContextMenuShortcut aria-hidden="true" className="ps-6 font-mono text-[11px] tracking-normal">
          {shortcut}
        </ContextMenuShortcut>
      )}
    </ContextMenuItem>
  )
}

/** Height of every row, in pixels. */
const rowHeight = 32

/** Column layout shared by the header and the rows; the type column shows once the pane is wide enough (4.14e), and a pane as narrow as a phone keeps only the name and size. */
const columns = 'grid grid-cols-[minmax(10rem,1fr)_88px_160px_112px] @max-lg:grid-cols-[minmax(0,1fr)_72px] @3xl:grid-cols-[minmax(10rem,1fr)_88px_150px_160px_112px] items-center gap-3 px-4'

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
  /** Called instead of `onOpen` when the entry opened with Enter or a double click is a file: download or edit it, as the user prefers. */
  onOpenFile?: (entry: RemoteEntry) => void
  /** Called when the user asks to go up one level with Backspace. */
  onUp: () => void
  /** Whether the server reports permissions; FTP does not, and its entries show a dash. */
  showPermissions: boolean
  /** Path of the selected entry; the first entry is selected when it is `null` or gone. */
  selectedPath: string | null
  /** Called when the selection moves to an entry. */
  onSelect: (entry: RemoteEntry) => void
  /** Called to download a file, or a folder as a ZIP, from the context menu; the item is left out without it. */
  onDownload?: (entry: RemoteEntry) => void
  /** Called to download the files of a folder one by one from the context menu; the item is left out without it. */
  onDownloadEach?: (entry: RemoteEntry) => void
  /** Called to open a file in the remote editor from the context menu; the item is left out without it. */
  onEdit?: (entry: RemoteEntry) => void
  /** Shows a file in the viewer (ADR-0018), with Space and from the menu; left out, neither is offered. */
  onPreview?: (entry: RemoteEntry) => void
  /** Called to rename an entry: F2 or the context menu; both are left out without it. */
  onRename?: (entry: RemoteEntry) => void
  /** Called to delete an entry: Delete or the context menu; both are left out without it. */
  onDelete?: (entry: RemoteEntry) => void
  /** Called to create a folder from the context menu; the item is left out without it. */
  onNewFolder?: () => void
  /** Whether folders can be dragged too; files always can. */
  dragFolders?: boolean
  /** Called when the user starts dragging an entry, to put what it carries in the drag data. */
  onDragEntry: (entry: RemoteEntry, data: DataTransfer) => void
  /** Text of the filter of the folder, whose words are highlighted in the names (4.14d). */
  highlight?: string
}

/** A name with the words of the filter highlighted. */
function HighlightedName({ name, query }: { name: string; query: string }) {
  const parts: ReactNode[] = []
  let at = 0
  for (const [start, end] of matchRanges(name, query)) {
    parts.push(name.slice(at, start), <mark key={start} className="rounded-sm bg-warn/30 text-foreground">{name.slice(start, end)}</mark>)
    at = end
  }
  parts.push(name.slice(at))
  return <span className="truncate">{parts}</span>
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
export function EntryTable({ entries, locale, timeZone, onOpen, onOpenFile, onUp, showPermissions, selectedPath, onSelect, onDownload, onDownloadEach, onEdit, onPreview, onRename, onDelete, onNewFolder, dragFolders, onDragEntry, highlight = '' }: EntryTableProps) {
  const { t } = useTranslation('explorer')
  const describeType = useDescribeType()
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
  const menuItems = active.type === 'directory' || onDownload || onEdit || onPreview || onRename || onDelete || onNewFolder

  const openEntry = (entry: RemoteEntry): void => (entry.type === 'file' && onOpenFile ? onOpenFile(entry) : onOpen(entry))

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    if (event.key === 'Enter') {
      event.preventDefault()
      openEntry(active)
      return
    }
    if (event.key === ' ' && onPreview && active.type === 'file') {
      event.preventDefault()
      onPreview(active)
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
        className="@container min-h-0 flex-1 overflow-auto outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      >
      <div
        role="row"
        className={cn(columns, 'sticky top-0 z-10 h-8 border-b bg-background text-[11px] font-medium tracking-wider text-muted-foreground uppercase')}
      >
        <span role="columnheader">{t(($) => $.columns.name)}</span>
        <span role="columnheader" className="text-end">
          {t(($) => $.columns.size)}
        </span>
        <span role="columnheader" className="@max-3xl:hidden">
          {t(($) => $.columns.type)}
        </span>
        <span role="columnheader" className="@max-lg:hidden">
          {t(($) => $.columns.modified)}
        </span>
        <span role="columnheader" className="@max-lg:hidden">
          {t(($) => $.columns.mode)}
        </span>
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
              onDoubleClick={() => openEntry(entry)}
              draggable={entry.type === 'file' || (dragFolders === true && entry.type === 'directory')}
              onDragStart={(event) => onDragEntry(entry, event.dataTransfer)}
              style={{ height: rowHeight, transform: `translateY(${item.start - rowHeight}px)` }}
              className={cn(
                columns,
                'absolute start-0 top-0 w-full cursor-pointer text-[13px] hover:bg-accent hover:**:text-foreground aria-selected:bg-primary/15 aria-selected:**:text-foreground',
              )}
            >
              <span role="gridcell" className="flex min-w-0 items-center gap-2">
                <FileTypeIcon type={fileTypeOf(entry)} />
                <HighlightedName name={entry.name} query={highlight} />
              </span>
              <span role="gridcell" className="text-end font-mono text-xs text-muted-foreground">
                {entry.type === 'directory' ? '—' : formatSize(entry.size)}
              </span>
              <span role="gridcell" className="truncate text-xs text-muted-foreground @max-3xl:hidden">
                {describeType(entry)}
              </span>
              <span role="gridcell" className="truncate text-xs text-muted-foreground @max-lg:hidden">
                {modified(entry, locale, timeZone)}
              </span>
              <span role="gridcell" className="font-mono text-xs text-muted-foreground @max-lg:hidden">
                {showPermissions ? entry.mode : '—'}
              </span>
            </div>
          )
        })}
      </div>
      </ContextMenuTrigger>
      {menuItems ? (
        <ContextMenuContent aria-label={t(($) => $.menu.label)} className="min-w-44">
          <ContextMenuGroup>
            {active.type === 'directory' && <MenuEntry icon={FolderOpen} label={t(($) => $.menu.open)} shortcut={t(($) => $.menu.keys.enter)} onClick={() => onOpen(active)} />}
            {active.type === 'file' && onPreview && <MenuEntry icon={Eye} label={t(($) => $.menu.preview)} shortcut={t(($) => $.menu.keys.space)} onClick={() => onPreview(active)} />}
            {active.type === 'file' && onEdit && <MenuEntry icon={FilePen} label={t(($) => $.menu.edit)} onClick={() => onEdit(active)} />}
          </ContextMenuGroup>
          {onDownload && (
            <>
              <ContextMenuSeparator />
              <ContextMenuGroup>
                {active.type === 'file' && <MenuEntry icon={Download} label={t(($) => $.menu.download)} onClick={() => onDownload(active)} />}
                {active.type === 'directory' && <MenuEntry icon={FileArchive} label={t(($) => $.menu.downloadZip)} onClick={() => onDownload(active)} />}
                {active.type === 'directory' && onDownloadEach && <MenuEntry icon={Files} label={t(($) => $.menu.downloadEach)} onClick={() => onDownloadEach(active)} />}
              </ContextMenuGroup>
            </>
          )}
          {(onRename || onDelete) && <ContextMenuSeparator />}
          {onRename && <MenuEntry icon={TextCursorInput} label={t(($) => $.menu.rename)} shortcut="F2" onClick={() => onRename(active)} />}
          {onDelete && <MenuEntry icon={Trash2} label={t(($) => $.menu.delete)} shortcut={t(($) => $.menu.keys.delete)} destructive onClick={() => onDelete(active)} />}
          {onNewFolder && <ContextMenuSeparator />}
          {onNewFolder && <MenuEntry icon={FolderPlus} label={t(($) => $.menu.newFolder)} onClick={onNewFolder} />}
        </ContextMenuContent>
      ) : null}
    </ContextMenu>
  )
}