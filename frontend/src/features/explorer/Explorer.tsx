import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUp, CircleAlert, CircleStop, FolderOpen, FolderPlus, FolderUp, Plug, RefreshCw, Search, SearchX, ShieldQuestion, Unplug, Upload } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'
import { Group, layoutStorage, Panel, ResizeHandle, useCollapsible, useDefaultLayout } from '@/components/ui/resizable'
import { LoadingBar } from '@/components/ui/loading-bar'
import { Loading } from '@/components/ui/loading'
import { cn } from '@/lib/utils'
import type { HostKeyTrustMode, RemoteEntry } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { uuid } from '@/lib/uuid'

import { useTimeZone } from '../auth/use-time-zone'
import { changedHostKeyOf, unknownHostKeyOf, type UnknownHostKey } from '../quick/errors'
import { useSitesData } from '../sites/use-sites-data'

import { BookmarksBar } from '../bookmarks/BookmarksBar'
import { focusFilterEvent } from '../palette/focus-filter'
import { BookmarksMenu } from '../bookmarks/BookmarksMenu'
import { Breadcrumbs } from './Breadcrumbs'
import { TimeZoneProbe } from './TimeZoneProbe'
import { localSiteId, readLocalFile } from '../local/local-fs'
import { useLocalRoot } from '../local/local-root-store'
import { readDropped, treeFromHandle, treeFromInput, TreeTooLarge, type UploadTree } from '../local/tree'
import { collectFiles, eachDelayMs, maxEachFiles, preparingTimeoutMs, startDownload, TooManyFiles } from './download'
import { isEntryDrag, readDrag, writeDrag, type DraggedEntry } from './entry-drag'
import { CopyPolicyDialog, type CopyPolicy } from './CopyPolicyDialog'
import type { TreePolicy } from './use-uploads'
import { UploadConflictDialog } from './UploadConflictDialog'
import { useUploads } from './use-uploads'
import { DeleteEntryDialog } from './DeleteEntryDialog'
import { EntryNameDialog } from './EntryNameDialog'
import { useConnectionState, useDisconnected } from '../toolbar/connection-store'

import { ChangedHostKeyNotice } from './ChangedHostKeyNotice'
import { ConnectingState } from './ExplorerLoading'
import { DownloadNotices, type RequestedDownload } from './DownloadNotices'
import { VaultLockedNotice } from '../vault/VaultLockedNotice'
import { CredentialNotice } from '../sites/CredentialNotice'
import { useUiStore } from '@/state/ui-store'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import { SearchDialog } from '../search/SearchDialog'
import { RemoteEditor } from '../editor/RemoteEditor'

import { EntryTable } from './EntryTable'
import { Inspector } from './Inspector'
import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'
import { joinPath, parentPath, resolveSelection, sortEntries } from './entries'
import { filesKey, filesPrefix, listingFreshMs, listingKeepMs } from './files-key'
import { emptyFilter, filterEntries, isActive, type EntryFilter } from './entry-filter'
import { FilterBox } from './FilterBox'
import { FilterMenu } from './FilterMenu'
import { FilterSummary } from './FilterSummary'

/** The dialog that is open over the explorer. */
type Dialog = { kind: 'newFolder' } | { kind: 'rename'; entry: RemoteEntry } | { kind: 'delete'; entry: RemoteEntry } | { kind: 'edit'; entry: RemoteEntry }

/** Props of {@link Explorer}. */
export interface ExplorerProps {
  /** Workspace of the connection. */
  workspaceId: string
  /** Saved site or quick connection to browse. */
  siteId: string
  /** Absolute path of the folder in view. */
  path: string
  /** Called with the folder the user moves to. */
  onNavigate: (path: string) => void
  /** Extra controls shown at the end of the toolbar. */
  actions?: ReactNode
  /** Which pane of the view this is; each pane remembers the width of its details panel on its own. */
  pane?: 'main' | 'second'
  /** Path of the entry selected when the explorer opens, as its tab remembered it. */
  initialSelection?: string
  /** Called with the path of the entry the user selects. */
  onSelectionChange?: (path: string) => void
  /** Disconnects the site, offered when the user refuses a changed server key; without it that choice is not offered. */
  onDisconnect?: () => void
}

/** Browser of the files of one connection: path, toolbar, a table of the folder in view and a resizable details panel beside it. */
export function Explorer({ workspaceId, siteId, path, onNavigate, actions, pane = 'main', initialSelection, onSelectionChange, onDisconnect }: ExplorerProps) {
  const { t, i18n } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const { sites, quick } = useSitesData(workspaceId)
  const [trustFailure, setTrustFailure] = useState<ExplorerErrorKey | null>(null)
  const [selectedPath, setSelection] = useState<string | null>(initialSelection ?? null)
  const setSelectedPath = (selection: string): void => {
    setSelection(selection)
    onSelectionChange?.(selection)
  }
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [dragging, setDragging] = useState<'upload' | 'copy' | null>(null)
  const [each, setEach] = useState<{ count: number } | ExplorerErrorKey | 'tooMany' | null>(null)
  const [folders, setFolders] = useState<UploadTree[]>([])
  const [asking, setAsking] = useState<{ source: DraggedEntry; name: string } | null>(null)
  const [copyFailure, setCopyFailure] = useState<ExplorerErrorKey | null>(null)
  const [searching, setSearching] = useState(false)
  const [preparing, setPreparing] = useState<RequestedDownload[]>([])
  const dismissDownload = useCallback((id: string) => setPreparing((list) => list.filter((item) => item.id !== id)), [])
  const fileDoubleClick = useUiStore((state) => state.fileDoubleClick)
  const picker = useRef<HTMLInputElement>(null)
  const folderPicker = useRef<HTMLInputElement>(null)
  const disconnected = useDisconnected({ workspaceId, siteId })
  const detailsOpen = useUiStore((state) => state.detailsOpen)
  const setDetailsOpen = useUiStore((state) => state.setDetailsOpen)
  const details = useCollapsible(detailsOpen, setDetailsOpen)
  const beside = useDefaultLayout({ id: `sentinel-details-${pane}`, storage: layoutStorage })
  const reconnect = useConnectionState((state) => state.reconnect)
  const focus = useConnectionState((state) => state.focus)
  const listing = useQuery({
    queryKey: filesKey(workspaceId, siteId, path),
    queryFn: ({ signal }) => api.files.list(workspaceId, siteId, path, signal),
    enabled: !disconnected,
    staleTime: listingFreshMs,
    gcTime: listingKeepMs,
    placeholderData: (previous, previousQuery) => (previousQuery?.queryKey[3] === siteId ? previous : undefined),
  })
  const opening = listing.isFetching && listing.isPlaceholderData
  const canceled = !listing.isFetching && (listing.isPending || listing.isPlaceholderData)
  const entries = useMemo(() => sortEntries(listing.data ?? []), [listing.data])
  const [filtered, setFiltered] = useState<{ path: string; filter: EntryFilter }>({ path, filter: emptyFilter })
  const filter = filtered.path === path ? filtered.filter : emptyFilter
  const setFilter = (next: EntryFilter): void => setFiltered({ path, filter: next })
  const filterField = useRef<HTMLInputElement>(null)
  const section = useRef<HTMLElement>(null)

  useEffect(() => {
    if (pane !== 'main') {
      return
    }
    const onKey = (event: KeyboardEvent): void => {
      if (event.key === '/' && event.target === document.body && filterField.current !== null) {
        event.preventDefault()
        filterField.current.focus()
      }
    }
    const onAsk = (): void => {
      window.setTimeout(() => (filterField.current as HTMLInputElement).focus())
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener(focusFilterEvent, onAsk)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener(focusFilterEvent, onAsk)
    }
  }, [pane])
  const target = [...sites, ...quick].find((connection) => connection.id === siteId)
  const savedSite = sites.find((saved) => saved.id === siteId)
  const showPermissions = target === undefined || target.protocol === 'sftp'
  const hostKey = unknownHostKeyOf(listing.error)
  const changedKey = changedHostKeyOf(listing.error)
  const timeZone = useTimeZone()
  const visible = useMemo(() => filterEntries(entries, filter, new Date(), timeZone), [entries, filter, timeZone])
  const filtering = isActive(filter)
  const failure = listing.error === null ? null : explorerErrorKeyOf(listing.error)
  const asksPassword = target !== undefined && (failure === 'credentials_required' || (failure === 'remote_auth_failed' && target.authMethod === 'ask'))

  const trust = async (key: UnknownHostKey, connection: { host: string; port: number }, mode?: HostKeyTrustMode): Promise<void> => {
    try {
      await api.hostKeys.trust(workspaceId, { host: connection.host, port: connection.port, keyType: key.keyType, fingerprint: key.fingerprint, mode })
    } catch (error) {
      setTrustFailure(explorerErrorKeyOf(error))
      return
    }
    setTrustFailure(null)
    await listing.refetch()
  }

  const reload = (): Promise<void> => queryClient.invalidateQueries({ queryKey: filesPrefix(workspaceId, siteId) })

  const uploads = useUploads({ api, workspaceId, siteId, path, onUploaded: reload })

  const download = (entry: RemoteEntry): void => {
    const id = uuid()
    setPreparing((list) => [...list, { id, name: entry.name }])
    setTimeout(() => dismissDownload(id), preparingTimeoutMs)
    startDownload(entry.type === 'directory' ? api.files.archiveUrl(workspaceId, siteId, entry.path, id) : api.files.downloadUrl(workspaceId, siteId, entry.path, id))
  }

  const edit = (entry: RemoteEntry): void => setDialog({ kind: 'edit', entry })

  const downloadEach = async (folder: RemoteEntry): Promise<void> => {
    try {
      const found = await collectFiles((dir) => api.files.list(workspaceId, siteId, dir), folder.path, maxEachFiles)
      setEach({ count: found.length })
      for (const file of found) {
        startDownload(api.files.downloadUrl(workspaceId, siteId, file.path, uuid()))
        await new Promise((resolve) => setTimeout(resolve, eachDelayMs))
      }
      setEach(null)
    } catch (error) {
      setEach(error instanceof TooManyFiles ? 'tooMany' : explorerErrorKeyOf(error))
    }
  }

  const copyWith = async (source: DraggedEntry, policy: CopyPolicy, verify: boolean): Promise<void> => {
    const name = source.path.slice(source.path.lastIndexOf('/') + 1)
    try {
      await api.copies.create(workspaceId, {
        sourceSiteId: source.siteId,
        sourcePath: source.path,
        targetSiteId: siteId,
        targetPath: joinPath(path, name),
        policy,
        verify,
        transferId: uuid(),
      })
      setCopyFailure(null)
    } catch (error) {
      setCopyFailure(explorerErrorKeyOf(error))
    }
  }

  const copyHere = async (source: DraggedEntry): Promise<void> => {
    const name = source.path.slice(source.path.lastIndexOf('/') + 1)
    if (source.workspaceId !== workspaceId || (source.siteId === siteId && parentPath(source.path) === path)) {
      return
    }
    const local = useLocalRoot.getState().root
    if (source.siteId === localSiteId) {
      if (local !== null) {
        try {
          if (source.isFolder) {
            setFolders([await treeFromHandle(local, source.path)])
          } else {
            await uploads.upload([await readLocalFile(local, source.path)])
          }
        } catch (error) {
          setCopyFailure(error instanceof TreeTooLarge ? 'tree_too_large' : 'unknown')
        }
      }
      return
    }
    if (source.isFolder || entries.some((existing) => existing.name === name)) {
      setAsking({ source, name })
      return
    }
    await copyWith(source, 'fail', false)
  }

  const drop = (event: DragEvent<HTMLElement>): void => {
    event.preventDefault()
    setDragging(null)
    if (isEntryDrag(event.dataTransfer)) {
      const source = readDrag(event.dataTransfer)
      if (source !== null) {
        void copyHere(source)
      }
      return
    }
    void readDropped(event.dataTransfer)
      .then((dropped) => {
        if (dropped.folders.length > 0) {
          setFolders(dropped.folders)
        }
        return uploads.upload(dropped.files)
      })
      .catch((error: unknown) => setCopyFailure(error instanceof TreeTooLarge ? 'tree_too_large' : 'unknown'))
  }

  const uploadFolders = (policy: TreePolicy): void => {
    const pending = folders
    setFolders([])
    void (async () => {
      for (const tree of pending) {
        await uploads.uploadFolder(tree, policy)
      }
    })()
  }

  const dragEntry = (entry: RemoteEntry, data: DataTransfer): void =>
    writeDrag(data, { workspaceId, siteId, path: entry.path, isFolder: entry.type === 'directory' })

  const open = (entry: { path: string }): void => {
    onNavigate(entry.path)
  }

  if (disconnected) {
    return (
      <section aria-label={t(($) => $.list)} className="flex min-h-0 min-w-0 flex-1 flex-col">
        <PanelState icon={Unplug} tone="muted" title={t(($) => $.disconnected.title, { name: target?.name ?? siteId })} description={t(($) => $.disconnected.body)}>
          <Button size="lg" className="min-w-52" onClick={() => reconnect({ workspaceId, siteId })}>
            <Plug aria-hidden="true" />
            {t(($) => $.disconnected.connect)}
          </Button>
        </PanelState>
      </section>
    )
  }

  return (
    <section
      ref={section}
      aria-label={t(($) => $.list)}
      onKeyDown={(event) => {
        const typingHere = event.target instanceof HTMLElement && (event.target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target.tagName))
        const find = (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'f'
        if (find || (event.key === '/' && !typingHere)) {
          event.preventDefault()
          ;(filterField.current as HTMLInputElement).focus()
        }
      }}
      onDragOver={(event) => {
        event.preventDefault()
        setDragging(isEntryDrag(event.dataTransfer) ? 'copy' : 'upload')
      }}
      onDragLeave={() => setDragging(null)}
      onDrop={drop}
      onPointerDownCapture={() => focus({ workspaceId, siteId })}
      onFocusCapture={() => focus({ workspaceId, siteId })}
      className="@container relative flex min-h-0 min-w-0 flex-1 flex-col"
    >
      {dragging !== null ? (
        <div className="pointer-events-none absolute inset-2 z-20 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-background/80 text-sm font-medium">
          {dragging === 'copy' ? t(($) => $.copy.dropHint) : t(($) => $.upload.dropHint)}
        </div>
      ) : null}
      <div className="flex h-11 shrink-0 items-center gap-2 overflow-x-auto border-b px-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.up)} disabled={path === '/'} onClick={() => onNavigate(parentPath(path))}>
          <ArrowUp />
        </Button>
        <Breadcrumbs path={path} onNavigate={onNavigate} />
        <div className="flex min-w-0 flex-1 items-center">
          {savedSite !== undefined && <BookmarksBar workspaceId={workspaceId} siteId={siteId} path={path} onNavigate={onNavigate} />}
        </div>
        {savedSite !== undefined && <BookmarksMenu workspaceId={workspaceId} siteId={siteId} siteName={savedSite.name} path={path} onNavigate={onNavigate} />}
        <span className="shrink-0 font-mono text-xs whitespace-nowrap text-muted-foreground @max-xl:hidden">{t(($) => $.rows, { count: entries.length })}</span>
        <FilterBox
          value={filter.text}
          onChange={(text) => setFilter({ ...filter, text })}
          onLeave={() => section.current?.querySelector<HTMLElement>('[role="grid"]')?.focus()}
          inputRef={filterField}
        />
        <FilterMenu filter={filter} onChange={setFilter} timeZone={timeZone} />
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          aria-label={t(($) => $.upload.fileInput)}
          onChange={(event) => {
            void uploads.upload(Array.from(event.target.files as FileList))
            event.target.value = ''
          }}
        />
        <Button variant="outline" size="sm" disabled={!listing.isSuccess || opening} onClick={() => (picker.current as HTMLInputElement).click()}>
          <Upload />
          <span className="@max-3xl:sr-only">{t(($) => $.toolbar.upload)}</span>
        </Button>
        <input
          ref={folderPicker}
          type="file"
          hidden
          aria-label={t(($) => $.upload.folderInput)}
          {...{ webkitdirectory: '' }}
          onChange={(event) => {
            const tree = treeFromInput(Array.from(event.target.files as FileList))
            if (tree !== null) {
              setFolders([tree])
            }
            event.target.value = ''
          }}
        />
        <Button variant="outline" size="sm" disabled={!listing.isSuccess || opening} onClick={() => (folderPicker.current as HTMLInputElement).click()}>
          <FolderUp />
          <span className="@max-3xl:sr-only">{t(($) => $.upload.folder)}</span>
        </Button>
        <Button variant="outline" size="sm" disabled={!listing.isSuccess || opening} onClick={() => setDialog({ kind: 'newFolder' })}>
          <FolderPlus />
          <span className="@max-3xl:sr-only">{t(($) => $.toolbar.newFolder)}</span>
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.refresh)}
          onClick={() => void reload()}
        >
          <RefreshCw />
        </Button>
        <Button variant="outline" size="sm" onClick={() => setSearching(true)}>
          <Search />
          <span className="@max-3xl:sr-only">{t(($) => $.search.title)}</span>
        </Button>
        {target !== undefined && target.protocol !== 'sftp' ? (
          <TimeZoneProbe workspaceId={workspaceId} siteId={siteId} path={path} site={sites.find((saved) => saved.id === siteId)} onApplied={() => void reload()} />
        ) : null}
        {actions}
      </div>
      {each === null ? null : typeof each === 'object' ? (
        <Loading label={t(($) => $.archive.working, { count: each.count })} className="mx-3 mt-3 rounded-lg border bg-card p-3 text-foreground" />
      ) : (
        <div role="alert" className="mx-3 mt-3 flex items-center justify-between gap-3 rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm">
          <span>{each === 'tooMany' ? t(($) => $.archive.tooMany, { limit: maxEachFiles }) : `${t(($) => $.archive.failure)}: ${t(($) => $.errors[each])}`}</span>
          <Button variant="outline" size="sm" onClick={() => setEach(null)}>
            {t(($) => $.archive.dismiss)}
          </Button>
        </div>
      )}
      {copyFailure === null ? null : (
        <div role="alert" className="mx-3 mt-3 flex items-center justify-between gap-3 rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm">
          <span>
            {t(($) => $.copy.failure)}: {t(($) => $.errors[copyFailure])}
          </span>
          <Button variant="outline" size="sm" onClick={() => setCopyFailure(null)}>
            {t(($) => $.copy.dismiss)}
          </Button>
        </div>
      )}
      {uploads.failures.length > 0 ? (
        <div role="alert" className="mx-3 mt-3 flex flex-col gap-1 rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm">
          <p className="font-medium text-destructive">{t(($) => $.upload.failures)}</p>
          <ul className="list-disc ps-5">
            {uploads.failures.map((failure, index) => (
              <li key={`${failure.name}-${index}`}>
                <span className="font-mono">{failure.name}</span>: {t(($) => $.errors[failure.key])}
              </li>
            ))}
          </ul>
          <Button variant="outline" size="sm" className="self-start" onClick={uploads.clearFailures}>
            {t(($) => $.upload.dismiss)}
          </Button>
        </div>
      ) : null}
      {uploads.conflict === null ? null : <UploadConflictDialog conflict={uploads.conflict} />}
      {folders.length === 0 ? null : (
        <CopyPolicyDialog
          allowed={['skip', 'rename', 'overwrite', 'fail']}
          name={folders.map((tree) => tree.name).join(', ')}
          onCancel={() => setFolders([])}
          onConfirm={(policy) => uploadFolders(policy as TreePolicy)}
        />
      )}
      {asking === null ? null : (
        <CopyPolicyDialog
          name={asking.name}
          onCancel={() => setAsking(null)}
          verifiable
          onConfirm={(policy, verify) => {
            setAsking(null)
            void copyWith(asking.source, policy, verify)
          }}
        />
      )}
      <LoadingBar active={listing.isFetching} label={t(($) => $.opening, { path })} />
      {listing.isPending && listing.isFetching && <ConnectingState name={target?.name ?? siteId} />}
      {canceled && (
        <PanelState icon={CircleStop} tone="muted" title={t(($) => $.canceled, { path })}>
          <Button variant="outline" onClick={() => void listing.refetch()}>
            {t(($) => $.retry)}
          </Button>
        </PanelState>
      )}
      {failure !== null && hostKey !== undefined && target !== undefined && (
        <PanelState icon={ShieldQuestion} tone="warn" role="alert" title={t(($) => $.trust.title)} description={t(($) => $.trust.body)}>
          <dl className="grid w-full grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 rounded-lg border bg-muted/40 px-4 py-2 text-start text-sm">
            <dt className="text-muted-foreground">{t(($) => $.trust.keyType)}</dt>
            <dd className="font-mono">{hostKey.keyType}</dd>
            <dt className="text-muted-foreground">{t(($) => $.trust.fingerprint)}</dt>
            <dd className="font-mono break-all">{hostKey.fingerprint}</dd>
          </dl>
          {trustFailure !== null && <p className="text-sm text-destructive">{t(($) => $.errors[trustFailure])}</p>}
          <Button size="lg" className="min-w-52" onClick={() => trust(hostKey, target)}>
            {t(($) => $.trust.confirm)}
          </Button>
        </PanelState>
      )}
      {failure !== null && changedKey !== undefined && target !== undefined && (
        <ChangedHostKeyNotice
          hostKey={changedKey}
          failure={trustFailure}
          onTrust={(mode) => trust(changedKey, target, mode)}
          onCancel={onDisconnect}
        />
      )}
      {asksPassword && target !== undefined && (
        <CredentialNotice
          key={listing.errorUpdateCount}
          workspaceId={workspaceId}
          target={target}
          refused={failure === 'remote_auth_failed'}
          onGiven={() => void listing.refetch()}
        />
      )}
      {failure === 'vault_locked' && (
        <VaultLockedNotice need="connect" layout="page" autoOpen target={target} onUnlocked={() => void listing.refetch()} />
      )}
      {failure !== null && failure !== 'vault_locked' && !asksPassword && ((hostKey === undefined && changedKey === undefined) || target === undefined) && (
        <PanelState icon={CircleAlert} tone="danger" role="alert" title={t(($) => $.failedTitle)}
          description={
            <>
              {t(($) => $.errors[failure])}
              <ErrorGuide failure={listing.error} className="mx-auto mt-4 max-w-sm" />
            </>
          }
        >
          <Button variant="outline" onClick={() => void listing.refetch()}>
            {t(($) => $.retry)}
          </Button>
        </PanelState>
      )}
      {filtering && listing.isSuccess && !canceled && entries.length > 0 && (
        <FilterSummary filter={filter} shown={visible.length} total={entries.length} listedAt={listing.dataUpdatedAt} onChange={setFilter} onRefresh={() => void reload()} />
      )}
      {listing.isSuccess && !canceled && !opening && entries.length === 0 && <PanelState icon={FolderOpen} tone="muted" role="none" title={t(($) => $.emptyDir)} />}
      {listing.isSuccess && !canceled && entries.length > 0 && visible.length === 0 && (
        <PanelState icon={SearchX} tone="muted" title={t(($) => $.filter.noneTitle)} description={t(($) => $.filter.noneBody, { total: entries.length })}>
          <Button variant="outline" onClick={() => setFilter(emptyFilter)}>
            {t(($) => $.filter.clear)}
          </Button>
        </PanelState>
      )}
      {listing.isSuccess && !canceled && visible.length > 0 && (
        <Group
          id={`sentinel-details-${pane}`}
          aria-busy={opening}
          className={cn('min-h-0 flex-1 transition-opacity', opening && 'pointer-events-none opacity-50')}
          {...beside}
        >
          <Panel id="entries" minSize={380} className="flex h-full min-h-0 min-w-0 flex-col">
              <EntryTable
                key={path}
                entries={visible}
                highlight={filter.text}
                locale={i18n.language}
                timeZone={timeZone}
                onOpen={open}
                onOpenFile={(entry) => (fileDoubleClick === 'edit' ? edit(entry) : download(entry))}
                onUp={() => onNavigate(parentPath(path))}
                showPermissions={showPermissions}
                selectedPath={selectedPath}
                onSelect={(entry) => setSelectedPath(entry.path)}
                onDownload={download}
                onDownloadEach={(entry) => void downloadEach(entry)}
                onEdit={edit}
                onRename={(entry) => setDialog({ kind: 'rename', entry })}
                onDelete={(entry) => setDialog({ kind: 'delete', entry })}
                onNewFolder={() => setDialog({ kind: 'newFolder' })}
                dragFolders
                onDragEntry={dragEntry}
              />
          </Panel>
          <ResizeHandle aria-label={t(($) => $.panels.details)} />
          <Panel id="details" collapsible collapsedSize={0} defaultSize={260} minSize={200} maxSize="45%" groupResizeBehavior="preserve-pixel-size" className="h-full" {...details}>
              <Inspector
                entry={visible[resolveSelection(visible, selectedPath)]}
                showPermissions={showPermissions}
                locale={i18n.language}
                timeZone={timeZone}
                onDownload={download}
                onDownloadEach={(entry) => void downloadEach(entry)}
                onChecksum={(entry, algorithm) => api.files.checksum(workspaceId, siteId, entry.path, algorithm)}
                onEdit={edit}
                onRename={(entry) => setDialog({ kind: 'rename', entry })}
                onDelete={(entry) => setDialog({ kind: 'delete', entry })}
              />
          </Panel>
        </Group>
      )}
      {dialog?.kind === 'newFolder' && (
        <EntryNameDialog
          mode="newFolder"
          initialName=""
          onSubmit={async (name) => {
            const created = await api.files.mkdir(workspaceId, siteId, joinPath(path, name))
            setSelectedPath(created.path)
            await reload()
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'rename' && (
        <EntryNameDialog
          mode="rename"
          initialName={dialog.entry.name}
          onSubmit={async (name) => {
            const renamed = await api.files.rename(workspaceId, siteId, dialog.entry.path, joinPath(path, name))
            setSelectedPath(renamed.path)
            await reload()
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'edit' && (
        <RemoteEditor
          key={dialog.entry.path}
          api={api}
          workspaceId={workspaceId}
          siteId={siteId}
          entry={dialog.entry}
          onClose={() => setDialog(null)}
          onSaved={() => void reload()}
        />
      )}
      {dialog?.kind === 'delete' && (
        <DeleteEntryDialog
          entry={dialog.entry}
          onDelete={async (recursive) => {
            await api.files.remove(workspaceId, siteId, dialog.entry.path, recursive)
            await reload()
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {searching && (
        <SearchDialog
          workspaceId={workspaceId}
          siteId={siteId}
          path={path}
          onOpen={(found) => {
            if (found !== path) {
              onNavigate(found)
            }
          }}
          onClose={() => setSearching(false)}
        />
      )}
      <DownloadNotices downloads={preparing} onDone={dismissDownload} />
    </section>
  )
}
