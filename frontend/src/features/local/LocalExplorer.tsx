import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUp, FolderOpen, RefreshCw } from 'lucide-react'
import { useMemo, useState, type DragEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { LoadingBar } from '@/components/ui/loading-bar'
import { useApi } from '@/lib/api/use-api'
import { ApiError } from '@/lib/transport'
import { uuid } from '@/lib/uuid'
import { cn } from '@/lib/utils'

import { Breadcrumbs } from '../explorer/Breadcrumbs'
import { EntryTable } from '../explorer/EntryTable'
import { isEntryDrag, readDrag, writeDrag, type DraggedEntry } from '../explorer/entry-drag'
import { parentPath, sortEntries } from '../explorer/entries'
import { explorerErrorKeyOf, type ExplorerErrorKey } from '../explorer/errors'
import { resolveTimeZone } from '@/lib/datetime'

import { listLocal, LocalNameTaken, localSiteId, localSupported, pickLocalDirectory, saveToLocal } from './local-fs'
import { useLocalRoot } from './local-root-store'

/** Props of {@link LocalExplorer}. */
export interface LocalExplorerProps {
  /** Workspace whose files the panes exchange with this folder. */
  workspaceId: string
  /** Absolute path of the folder in view, below the folder the user chose. */
  path: string
  /** Called with the folder the user moves to. */
  onNavigate: (path: string) => void
  /** Extra controls shown at the end of the toolbar. */
  actions?: ReactNode
}

/**
 * Pane with a folder of the user's computer, opened through the browser's folder picker.
 *
 * Files dragged from a remote pane onto it are saved in the folder in view, and its files can be dragged onto a remote pane
 * to upload them. It never overwrites a local file and cannot reach outside the folder the user chose.
 */
export function LocalExplorer({ workspaceId, path, onNavigate, actions }: LocalExplorerProps) {
  const { t, i18n } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const root = useLocalRoot((state) => state.root)
  const version = useLocalRoot((state) => state.version)
  const setRoot = useLocalRoot((state) => state.setRoot)
  const [selectedPath, setSelectedPath] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const [failure, setFailure] = useState<ExplorerErrorKey | 'localFailure' | null>(null)
  const listing = useQuery({
    queryKey: ['local', version, path],
    queryFn: () => listLocal(root as FileSystemDirectoryHandle, path),
    enabled: root !== null,
  })
  const entries = useMemo(() => sortEntries(listing.data ?? []), [listing.data])
  const timeZone = resolveTimeZone()

  const choose = async (): Promise<void> => {
    const chosen = await pickLocalDirectory()
    if (chosen !== null) {
      setRoot(chosen)
      onNavigate('/')
    }
  }

  const reload = (): Promise<void> => queryClient.invalidateQueries({ queryKey: ['local', version, path] })

  const save = async (source: DraggedEntry): Promise<void> => {
    const name = source.path.slice(source.path.lastIndexOf('/') + 1)
    if (root === null || source.isFolder || source.siteId === localSiteId || source.workspaceId !== workspaceId) {
      return
    }
    try {
      const stream = await api.files.download(workspaceId, source.siteId, source.path, uuid())
      await saveToLocal(root, path, name, stream)
      setFailure(null)
      await reload()
    } catch (error) {
      setFailure(error instanceof LocalNameTaken ? 'path_exists' : error instanceof ApiError ? explorerErrorKeyOf(error) : 'localFailure')
    }
  }

  const drop = (event: DragEvent<HTMLElement>): void => {
    event.preventDefault()
    setDragging(false)
    const source = readDrag(event.dataTransfer)
    if (source !== null) {
      void save(source)
    }
  }

  return (
    <section
      aria-label={t(($) => $.local.pane)}
      onDragOver={(event) => {
        event.preventDefault()
        setDragging(isEntryDrag(event.dataTransfer))
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={drop}
      className="relative flex min-h-0 flex-1 flex-col"
    >
      {dragging ? (
        <div className="pointer-events-none absolute inset-2 z-20 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-background/80 text-sm font-medium">
          {t(($) => $.local.dropHint)}
        </div>
      ) : null}
      <div className="flex h-11 shrink-0 items-center gap-2 border-b px-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.up)} disabled={root === null || path === '/'} onClick={() => onNavigate(parentPath(path))}>
          <ArrowUp />
        </Button>
        {root === null ? <span className="flex-1" /> : <Breadcrumbs path={path} onNavigate={onNavigate} />}
        {root === null ? null : <span className="font-mono text-xs text-muted-foreground">{t(($) => $.rows, { count: entries.length })}</span>}
        <Button variant="ghost" size="sm" disabled={!localSupported()} onClick={() => void choose()}>
          <FolderOpen />
          {root === null ? t(($) => $.local.choose) : t(($) => $.local.change)}
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.refresh)} disabled={root === null} onClick={() => void reload()}>
          <RefreshCw />
        </Button>
        {actions}
      </div>
      {failure === null ? null : (
        <div role="alert" className="mx-3 mt-3 flex items-center justify-between gap-3 rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm">
          <span>
            {t(($) => $.local.failure)}: {failure === 'localFailure' ? t(($) => $.errors.unknown) : t(($) => $.errors[failure])}
          </span>
          <Button variant="outline" size="sm" onClick={() => setFailure(null)}>
            {t(($) => $.copy.dismiss)}
          </Button>
        </div>
      )}
      <LoadingBar active={listing.isFetching} label={t(($) => $.opening, { path })} />
      {!localSupported() && <p role="status" className="p-4 text-sm text-muted-foreground">{t(($) => $.local.unsupported)}</p>}
      {localSupported() && root === null && <p className="p-4 text-sm text-muted-foreground">{t(($) => $.local.hint)}</p>}
      {listing.isError && (
        <p role="alert" className="p-4 text-sm text-destructive">
          {t(($) => $.local.readFailure)}
        </p>
      )}
      {listing.isSuccess && entries.length === 0 && <p className="p-4 text-sm text-muted-foreground">{t(($) => $.local.empty)}</p>}
      {listing.isSuccess && entries.length > 0 && (
        <div className={cn('flex min-h-0 flex-1')}>
          <EntryTable
            key={path}
            entries={entries}
            locale={i18n.language}
            timeZone={timeZone}
            onOpen={(entry) => (entry.type === 'directory' ? onNavigate(entry.path) : undefined)}
            onUp={() => onNavigate(parentPath(path))}
            showPermissions={false}
            selectedPath={selectedPath}
            onSelect={(entry) => setSelectedPath(entry.path)}
            onDragEntry={(entry, data) => writeDrag(data, { workspaceId, siteId: localSiteId, path: entry.path, isFolder: false })}
          />
        </div>
      )}
    </section>
  )
}
