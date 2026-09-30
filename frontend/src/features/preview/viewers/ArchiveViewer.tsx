import { useQuery } from '@tanstack/react-query'
import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowLeft, ChevronRight, CircleAlert, Download, FileArchive, Search, TriangleAlert } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Loading } from '@/components/ui/loading'
import { PanelState } from '@/components/ui/panel-state'
import { Tip } from '@/components/ui/tip'
import type { ArchiveEntry } from '@/lib/api'
import { formatInstant, resolveTimeZone } from '@/lib/datetime'
import { cn } from '@/lib/utils'

import { ErrorGuide } from '../../errors/ErrorGuide'
import { formatSize } from '../../explorer/entries'
import { FileTypeIcon } from '../../explorer/FileTypeIcon'
import { fileTypeOf } from '../../explorer/file-types'
import { LocalArchiveUnsupported } from '../local-archive'
import { PreviewBody } from '../PreviewBody'
import { EntryTooLarge, maxEntryBytes, type ArchiveAccess, type PreviewSource } from '../source'

import { buildTree, initiallyOpen, looksLikeBomb, totals, visibleRows, type ArchiveNode } from './archive-tree'
import { HexViewer } from './HexViewer'
import type { ViewerProps } from './types'

/** Height of a row of the tree, in pixels. */
const rowHeight = 28

/** The columns of the tree: name, size, compression and time. */
const columns = 'grid grid-cols-[minmax(0,1fr)_6rem_7rem_10rem] items-center gap-3 px-3'

/** Shows the failure of a request: the guidance of its code, when it has one. */
function Failure({ error, title, onRetry }: { error: unknown; title: string; onRetry?: () => void }) {
  const { t } = useTranslation('preview')
  return (
    <PanelState icon={CircleAlert} tone="danger" role="alert" title={title}>
      <ErrorGuide failure={error} />
      {onRetry !== undefined && (
        <Button variant="outline" onClick={onRetry}>
          {t(($) => $.retry)}
        </Button>
      )}
    </PanelState>
  )
}

/** Props of {@link EntryView}. */
interface EntryViewProps {
  /** The archive the entry belongs to. */
  archive: ArchiveAccess
  /** Stable identity of the archive. */
  archiveKey: string
  /** Name of the archive, to go back to. */
  archiveName: string
  /** The entry shown. */
  entry: ArchiveEntry
  /** Goes back to the tree. */
  onBack: () => void
}

/** One entry of an archive, opened with the viewer of its kind under a bar that goes back to the tree and downloads the entry. */
function EntryView({ archive, archiveKey, archiveName, entry, onBack }: EntryViewProps) {
  const { t } = useTranslation('preview')
  const opened = useQuery({
    queryKey: ['preview', archiveKey, 'entry', entry.name],
    queryFn: () => archive.open(entry),
    staleTime: Infinity,
    retry: false,
  })
  const download = archive.downloadUrl?.(entry)
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => root.current?.focus(), [])
  const onKeyDown = (event: KeyboardEvent): void => {
    if (event.key === 'Backspace' && !(event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement)) {
      event.preventDefault()
      onBack()
    }
  }
  let body
  if (opened.isPending) {
    body = <Loading label={t(($) => $.loading)} className="m-auto" />
  } else if (opened.error instanceof EntryTooLarge) {
    body = (
      <PanelState icon={FileArchive} tone="warn" title={t(($) => $.tooLarge.title)} description={t(($) => $.archive.entryTooLarge, { limit: formatSize(maxEntryBytes) })} />
    )
  } else if (opened.isError) {
    body = <Failure error={opened.error} title={t(($) => $.archive.entryFailed)} onRetry={() => void opened.refetch()} />
  } else {
    body = <PreviewBody key={opened.data.key} source={opened.data} />
  }
  return (
    <div ref={root} tabIndex={-1} className="flex min-h-0 flex-1 flex-col outline-none" onKeyDown={onKeyDown}>
      <div className="flex items-center gap-2 border-b bg-muted/40 px-2 py-1 text-xs">
        <Button variant="ghost" size="xs" onClick={onBack}>
          <ArrowLeft />
          {archiveName}
        </Button>
        <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground" title={entry.name}>
          {entry.name}
        </span>
        {download !== undefined && (
          <Tip label={t(($) => $.archive.downloadEntry)}>
            <a href={download} download aria-label={t(($) => $.archive.downloadEntry)} className={buttonVariants({ variant: 'ghost', size: 'icon-xs' })}>
              <Download />
            </a>
          </Tip>
        )}
      </div>
      {body}
    </div>
  )
}

/** Props of {@link ArchiveTree}. */
interface ArchiveTreeProps {
  /** The archive shown. */
  source: PreviewSource
  /** Its entries. */
  entries: readonly ArchiveEntry[]
  /** Its format, as the server named it. */
  format: string
  /** Whether it holds more entries than listed. */
  truncated: boolean
  /** Opens a file entry. */
  onOpen: (entry: ArchiveEntry) => void
}

/** The tree of an archive, with its totals, a filter, and one row per folder or file drawn only while in view. */
function ArchiveTree({ source, entries, format, truncated, onOpen }: ArchiveTreeProps) {
  const { t, i18n } = useTranslation('preview')
  const root = useMemo(() => buildTree(entries, i18n.language), [entries, i18n.language])
  const [open, setOpen] = useState(() => initiallyOpen(root))
  const [filter, setFilter] = useState('')
  const [selected, setSelected] = useState(0)
  const rows = useMemo(() => visibleRows(root, open, filter), [root, open, filter])
  const scroller = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => rowHeight, overscan: 12, scrollPaddingStart: rowHeight })
  const timeZone = resolveTimeZone()
  const sum = totals(entries)
  const current = Math.min(selected, rows.length - 1)

  const activate = (node: ArchiveNode): void => {
    if (node.folder) {
      const next = new Set(open)
      if (!next.delete(node.path)) {
        next.add(node.path)
      }
      setOpen(next)
    } else if (node.entry?.type === 'file') {
      onOpen(node.entry)
    }
  }
  const move = (index: number): void => {
    const next = Math.max(0, Math.min(index, rows.length - 1))
    setSelected(next)
    virtualizer.scrollToIndex(next)
  }
  const onKeyDown = (event: KeyboardEvent): void => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      move(current + (event.key === 'ArrowDown' ? 1 : -1))
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      activate(rows[current].node)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-3 border-b px-3 py-1 text-xs text-muted-foreground">
        <span className="rounded bg-muted px-1.5 py-0.5 font-mono uppercase">{format}</span>
        <span>{t(($) => $.archive.summary, { count: sum.files, size: formatSize(sum.bytes) })}</span>
        {truncated && (
          <span className="flex items-center gap-1 text-warn">
            <TriangleAlert aria-hidden="true" className="size-3.5" />
            {t(($) => $.archive.truncated, { count: entries.length })}
          </span>
        )}
        <label className="relative ms-auto w-56">
          <Search aria-hidden="true" className="pointer-events-none absolute start-2 top-1/2 size-3.5 -translate-y-1/2" />
          <Input
            value={filter}
            onChange={(event) => (setFilter(event.target.value), setSelected(0))}
            placeholder={t(($) => $.archive.filter)}
            aria-label={t(($) => $.archive.filter)}
            className="h-6 ps-7 text-xs"
          />
        </label>
      </div>
      {rows.length === 0 ? (
        <p className="m-auto text-sm text-muted-foreground">{filter === '' ? t(($) => $.archive.empty) : t(($) => $.archive.noMatches)}</p>
      ) : (
        <div
          ref={scroller}
          role="tree"
          tabIndex={0}
          aria-label={t(($) => $.archive.label, { name: source.name })}
          aria-activedescendant={`archive-row-${current}`}
          className="min-h-0 flex-1 overflow-auto bg-card text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
          onKeyDown={onKeyDown}
        >
          <div aria-hidden="true" className={cn(columns, 'sticky top-0 z-10 border-b bg-muted font-medium text-muted-foreground')} style={{ height: rowHeight }}>
            <span>{t(($) => $.archive.columns.name)}</span>
            <span className="text-end">{t(($) => $.archive.columns.size)}</span>
            <span className="text-end">{t(($) => $.archive.columns.packed)}</span>
            <span className="text-end">{t(($) => $.archive.columns.modified)}</span>
          </div>
          <div className="relative" style={{ height: virtualizer.getTotalSize() }}>
            {virtualizer.getVirtualItems().map((item) => {
              const { node, depth } = rows[item.index]
              const entry = node.entry
              return (
                <div
                  key={item.key}
                  id={`archive-row-${item.index}`}
                  role="treeitem"
                  aria-label={node.name}
                  aria-level={depth + 1}
                  aria-expanded={node.folder ? filter !== '' || open.has(node.path) : undefined}
                  aria-selected={item.index === current}
                  className={cn(
                    columns,
                    'absolute inset-x-0 cursor-default border-b',
                    item.index === current ? 'bg-accent text-accent-foreground' : 'hover:bg-muted/60',
                  )}
                  style={{ top: item.start, height: rowHeight }}
                  onClick={() => setSelected(item.index)}
                  onDoubleClick={() => activate(node)}
                >
                  <span className="flex min-w-0 items-center gap-1.5" style={{ paddingInlineStart: depth * 16 }}>
                    {node.folder ? (
                      <button
                        type="button"
                        tabIndex={-1}
                        aria-label={open.has(node.path) ? t(($) => $.tree.collapse) : t(($) => $.tree.expand)}
                        className="rounded hover:bg-muted"
                        onClick={() => activate(node)}
                      >
                        <ChevronRight aria-hidden="true" className={cn('size-3.5 transition-transform', (filter !== '' || open.has(node.path)) && 'rotate-90')} />
                      </button>
                    ) : (
                      <span className="w-3.5 shrink-0" />
                    )}
                    <FileTypeIcon type={fileTypeOf({ name: node.name, type: node.folder ? 'directory' : entry?.type === 'symlink' ? 'symlink' : 'file' })} className="size-4 shrink-0" />
                    <span className="truncate" title={entry?.name ?? node.path}>
                      {node.name}
                    </span>
                    {entry !== undefined && looksLikeBomb(entry) && (
                      <Tip label={t(($) => $.archive.bombHint)}>
                        <span className="flex shrink-0 items-center gap-1 rounded bg-destructive/15 px-1 text-[10px] text-destructive">
                          <TriangleAlert aria-hidden="true" className="size-3" />
                          {t(($) => $.archive.bomb)}
                        </span>
                      </Tip>
                    )}
                    {node.unsafe && (
                      <Tip label={t(($) => $.archive.unsafeHint)}>
                        <span className="flex shrink-0 items-center gap-1 rounded bg-warn/15 px-1 text-[10px] text-warn">
                          <TriangleAlert aria-hidden="true" className="size-3" />
                          {t(($) => $.archive.unsafe)}
                        </span>
                      </Tip>
                    )}
                  </span>
                  <span className="text-end tabular-nums text-muted-foreground">{entry !== undefined && !node.folder && entry.size >= 0 ? formatSize(entry.size) : ''}</span>
                  <span className="text-end tabular-nums text-muted-foreground">
                    {entry !== undefined && entry.size > 0 && entry.packed >= 0 ? t(($) => $.archive.ratio, { percent: Math.max(1, Math.round((entry.packed / entry.size) * 100)) }) : ''}
                  </span>
                  <span className="truncate text-end tabular-nums text-muted-foreground">
                    {entry?.modified !== undefined ? formatInstant(entry.modified, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' }) : ''}
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

/** Lists an archive and shows either its tree or the entry opened from it. */
function ArchiveBrowser({ source, archive }: { source: PreviewSource; archive: ArchiveAccess }) {
  const { t } = useTranslation('preview')
  const [entry, setEntry] = useState<ArchiveEntry | null>(null)
  const trees = useRef<HTMLDivElement>(null)
  const back = (): void => {
    setEntry(null)
    requestAnimationFrame(() => trees.current?.querySelector<HTMLElement>('[role="tree"]')?.focus())
  }
  const listing = useQuery({
    queryKey: ['preview', source.key, 'entries'],
    queryFn: ({ signal }) => archive.list(signal),
    staleTime: Infinity,
    retry: false,
  })
  if (listing.isPending) {
    return <Loading label={t(($) => $.archive.loading)} className="m-auto" />
  }
  if (listing.error instanceof LocalArchiveUnsupported) {
    return <PanelState icon={FileArchive} tone="warn" title={t(($) => $.archive.localUnsupported)} description={t(($) => $.archive.localUnsupportedBody)} />
  }
  if (listing.isError) {
    return <Failure error={listing.error} title={t(($) => $.archive.failed)} onRetry={() => void listing.refetch()} />
  }
  return (
    <>
      <div ref={trees} className={cn('flex min-h-0 flex-1 flex-col', entry !== null && 'hidden')}>
        <ArchiveTree source={source} entries={listing.data.entries} format={listing.data.format} truncated={listing.data.truncated} onOpen={setEntry} />
      </div>
      {entry !== null && <EntryView archive={archive} archiveKey={source.key} archiveName={source.name} entry={entry} onBack={back} />}
    </>
  )
}

/**
 * Shows the contents of an archive as a tree of folders and files, read without downloading it (D2.5): its format, totals and a filter,
 * the size, compression and time of each entry, and a mark on names that would be dangerous to extract. Enter, Space or a double click
 * opens a file with the viewer of its kind, an archive inside the archive included; Backspace goes back to the tree. A file with no way
 * to list its entries shows as bytes.
 */
export function ArchiveViewer({ source }: ViewerProps) {
  if (source.archive === undefined) {
    return <HexViewer source={source} kind="binary" />
  }
  return <ArchiveBrowser source={source} archive={source.archive} />
}
