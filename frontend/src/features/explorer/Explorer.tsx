import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUp, FolderPlus, RefreshCw, ShieldQuestion, Upload } from 'lucide-react'
import { useMemo, useRef, useState, type DragEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { LoadingBar } from '@/components/ui/loading-bar'
import { cn } from '@/lib/utils'
import type { RemoteEntry } from '@/lib/api'
import { resolveTimeZone } from '@/lib/datetime'
import { useApi } from '@/lib/api/use-api'
import { uuid } from '@/lib/uuid'

import { useSession } from '../auth/use-session'
import { unknownHostKeyOf, type UnknownHostKey } from '../quick/errors'
import { useSitesData } from '../sites/use-sites-data'

import { Breadcrumbs } from './Breadcrumbs'
import { localSiteId, readLocalFile } from '../local/local-fs'
import { useLocalRoot } from '../local/local-root-store'
import { collectFiles, eachDelayMs, maxEachFiles, startDownload, TooManyFiles } from './download'
import { isEntryDrag, readDrag, writeDrag, type DraggedEntry } from './entry-drag'
import { CopyPolicyDialog, type CopyPolicy } from './CopyPolicyDialog'
import { UploadConflictDialog } from './UploadConflictDialog'
import { useUploads } from './use-uploads'
import { DeleteEntryDialog } from './DeleteEntryDialog'
import { EntryNameDialog } from './EntryNameDialog'
import { ConnectingState } from './ExplorerLoading'
import { EntryTable } from './EntryTable'
import { Inspector } from './Inspector'
import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'
import { joinPath, parentPath, resolveSelection, sortEntries } from './entries'
import { filesKey } from './files-key'

/** The dialog that is open over the explorer. */
type Dialog = { kind: 'newFolder' } | { kind: 'rename'; entry: RemoteEntry } | { kind: 'delete'; entry: RemoteEntry }

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
}

/** Browser of the files of one connection: path, toolbar and a table of the folder in view. */
export function Explorer({ workspaceId, siteId, path, onNavigate, actions }: ExplorerProps) {
  const { t, i18n } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const session = useSession()
  const { sites, quick } = useSitesData(workspaceId)
  const [trustFailure, setTrustFailure] = useState<ExplorerErrorKey | null>(null)
  const [selectedPath, setSelectedPath] = useState<string | null>(null)
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [dragging, setDragging] = useState<'upload' | 'copy' | null>(null)
  const [each, setEach] = useState<{ count: number } | ExplorerErrorKey | 'tooMany' | null>(null)
  const [asking, setAsking] = useState<{ source: DraggedEntry; name: string } | null>(null)
  const [copyFailure, setCopyFailure] = useState<ExplorerErrorKey | null>(null)
  const picker = useRef<HTMLInputElement>(null)
  const listing = useQuery({
    queryKey: filesKey(workspaceId, siteId, path),
    queryFn: () => api.files.list(workspaceId, siteId, path),
    placeholderData: (previous, previousQuery) => (previousQuery?.queryKey[3] === siteId ? previous : undefined),
  })
  const opening = listing.isFetching && listing.isPlaceholderData
  const entries = useMemo(() => sortEntries(listing.data ?? []), [listing.data])
  const target = [...sites, ...quick].find((connection) => connection.id === siteId)
  const showPermissions = target === undefined || target.protocol === 'sftp'
  const hostKey = unknownHostKeyOf(listing.error)
  const timeZone = session?.user.timezone ?? resolveTimeZone()
  const failure = listing.error === null ? null : explorerErrorKeyOf(listing.error)

  const trust = async (key: UnknownHostKey, connection: { host: string; port: number }): Promise<void> => {
    try {
      await api.hostKeys.trust(workspaceId, { host: connection.host, port: connection.port, ...key })
    } catch (error) {
      setTrustFailure(explorerErrorKeyOf(error))
      return
    }
    setTrustFailure(null)
    await listing.refetch()
  }

  const reload = (): Promise<void> => queryClient.invalidateQueries({ queryKey: filesKey(workspaceId, siteId, path) })

  const uploads = useUploads({ api, workspaceId, siteId, path, onUploaded: reload })

  const download = (entry: RemoteEntry): void =>
    startDownload(
      entry.type === 'directory' ? api.files.archiveUrl(workspaceId, siteId, entry.path, uuid()) : api.files.downloadUrl(workspaceId, siteId, entry.path, uuid()),
    )

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

  const copyWith = async (source: DraggedEntry, policy: CopyPolicy): Promise<void> => {
    const name = source.path.slice(source.path.lastIndexOf('/') + 1)
    try {
      await api.copies.create(workspaceId, {
        sourceSiteId: source.siteId,
        sourcePath: source.path,
        targetSiteId: siteId,
        targetPath: joinPath(path, name),
        policy,
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
      if (local !== null && !source.isFolder) {
        try {
          await uploads.upload([await readLocalFile(local, source.path)])
        } catch {
          setCopyFailure('unknown')
        }
      }
      return
    }
    if (source.isFolder || entries.some((existing) => existing.name === name)) {
      setAsking({ source, name })
      return
    }
    await copyWith(source, 'fail')
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
    void uploads.upload(Array.from(event.dataTransfer.files))
  }

  const dragEntry = (entry: RemoteEntry, data: DataTransfer): void =>
    writeDrag(data, { workspaceId, siteId, path: entry.path, isFolder: entry.type === 'directory' })

  const open = (entry: { type: string; path: string }): void => {
    if (entry.type === 'directory') {
      onNavigate(entry.path)
    }
  }

  return (
    <section
      aria-label={t(($) => $.list)}
      onDragOver={(event) => {
        event.preventDefault()
        setDragging(isEntryDrag(event.dataTransfer) ? 'copy' : 'upload')
      }}
      onDragLeave={() => setDragging(null)}
      onDrop={drop}
      className="relative flex min-h-0 flex-1 flex-col"
    >
      {dragging !== null ? (
        <div className="pointer-events-none absolute inset-2 z-20 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-background/80 text-sm font-medium">
          {dragging === 'copy' ? t(($) => $.copy.dropHint) : t(($) => $.upload.dropHint)}
        </div>
      ) : null}
      <div className="flex h-11 shrink-0 items-center gap-2 border-b px-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.up)} disabled={path === '/'} onClick={() => onNavigate(parentPath(path))}>
          <ArrowUp />
        </Button>
        <Breadcrumbs path={path} onNavigate={onNavigate} />
        <span className="font-mono text-xs text-muted-foreground">{t(($) => $.rows, { count: entries.length })}</span>
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
        <Button variant="ghost" size="sm" disabled={!listing.isSuccess || opening} onClick={() => (picker.current as HTMLInputElement).click()}>
          <Upload />
          {t(($) => $.toolbar.upload)}
        </Button>
        <Button variant="ghost" size="sm" disabled={!listing.isSuccess || opening} onClick={() => setDialog({ kind: 'newFolder' })}>
          <FolderPlus />
          {t(($) => $.toolbar.newFolder)}
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.refresh)}
          onClick={() => void reload()}
        >
          <RefreshCw />
        </Button>
        {actions}
      </div>
      {each === null ? null : typeof each === 'object' ? (
        <p role="status" className="mx-3 mt-3 rounded-lg border bg-card p-3 text-sm">
          {t(($) => $.archive.working, { count: each.count })}
        </p>
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
      {asking === null ? null : (
        <CopyPolicyDialog
          name={asking.name}
          onCancel={() => setAsking(null)}
          onConfirm={(policy) => {
            setAsking(null)
            void copyWith(asking.source, policy)
          }}
        />
      )}
      <LoadingBar active={listing.isFetching} label={t(($) => $.opening, { path })} />
      {listing.isPending && <ConnectingState name={target?.name ?? siteId} />}
      {failure !== null && hostKey !== undefined && target !== undefined && (
        <div role="alert" className="m-4 flex max-w-xl flex-col gap-3 rounded-xl border bg-card p-4 text-sm">
          <p className="flex items-center gap-2 font-medium">
            <ShieldQuestion className="size-5 text-warn" />
            {t(($) => $.trust.title)}
          </p>
          <p className="text-muted-foreground">{t(($) => $.trust.body)}</p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
            <dt className="text-muted-foreground">{t(($) => $.trust.keyType)}</dt>
            <dd className="font-mono">{hostKey.keyType}</dd>
            <dt className="text-muted-foreground">{t(($) => $.trust.fingerprint)}</dt>
            <dd className="font-mono break-all">{hostKey.fingerprint}</dd>
          </dl>
          {trustFailure !== null && <p className="text-destructive">{t(($) => $.errors[trustFailure])}</p>}
          <Button className="self-start" onClick={() => trust(hostKey, target)}>
            {t(($) => $.trust.confirm)}
          </Button>
        </div>
      )}
      {failure !== null && (hostKey === undefined || target === undefined) && (
        <div role="alert" className="m-4 flex max-w-xl flex-col gap-3 rounded-xl border border-destructive/40 bg-destructive/10 p-4 text-sm">
          <p className="text-destructive">{t(($) => $.errors[failure])}</p>
          <Button variant="outline" className="self-start" onClick={() => void listing.refetch()}>
            {t(($) => $.retry)}
          </Button>
        </div>
      )}
      {listing.isSuccess && !opening && entries.length === 0 && <p className="p-4 text-sm text-muted-foreground">{t(($) => $.emptyDir)}</p>}
      {listing.isSuccess && entries.length > 0 && (
        <div aria-busy={opening} className={cn('flex min-h-0 flex-1 transition-opacity', opening && 'pointer-events-none opacity-50')}>
          <EntryTable
            key={path}
            entries={entries}
            locale={i18n.language}
            timeZone={timeZone}
            onOpen={open}
            onUp={() => onNavigate(parentPath(path))}
            showPermissions={showPermissions}
            selectedPath={selectedPath}
            onSelect={(entry) => setSelectedPath(entry.path)}
            onDownload={download}
            onDownloadEach={(entry) => void downloadEach(entry)}
            onRename={(entry) => setDialog({ kind: 'rename', entry })}
            onDelete={(entry) => setDialog({ kind: 'delete', entry })}
            onNewFolder={() => setDialog({ kind: 'newFolder' })}
            dragFolders
            onDragEntry={dragEntry}
          />
          <Inspector
            entry={entries[resolveSelection(entries, selectedPath)]}
            showPermissions={showPermissions}
            locale={i18n.language}
            timeZone={timeZone}
            onDownload={download}
            onDownloadEach={(entry) => void downloadEach(entry)}
            onRename={(entry) => setDialog({ kind: 'rename', entry })}
            onDelete={(entry) => setDialog({ kind: 'delete', entry })}
          />
        </div>
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
    </section>
  )
}
