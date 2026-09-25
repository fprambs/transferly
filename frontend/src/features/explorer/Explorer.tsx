import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUp, FolderPlus, RefreshCw, ShieldQuestion, Upload } from 'lucide-react'
import { useMemo, useRef, useState, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import type { RemoteEntry } from '@/lib/api'
import { resolveTimeZone } from '@/lib/datetime'
import { useApi } from '@/lib/api/use-api'
import { uuid } from '@/lib/uuid'

import { useSession } from '../auth/use-session'
import { unknownHostKeyOf, type UnknownHostKey } from '../quick/errors'
import { useSitesData } from '../sites/use-sites-data'

import { Breadcrumbs } from './Breadcrumbs'
import { startDownload } from './download'
import { UploadConflictDialog } from './UploadConflictDialog'
import { useUploads } from './use-uploads'
import { DeleteEntryDialog } from './DeleteEntryDialog'
import { EntryNameDialog } from './EntryNameDialog'
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
}

/** Browser of the files of one connection: path, toolbar and a table of the folder in view. */
export function Explorer({ workspaceId, siteId, path, onNavigate }: ExplorerProps) {
  const { t, i18n } = useTranslation('explorer')
  const api = useApi()
  const queryClient = useQueryClient()
  const session = useSession()
  const { sites, quick } = useSitesData(workspaceId)
  const [trustFailure, setTrustFailure] = useState<ExplorerErrorKey | null>(null)
  const [selectedPath, setSelectedPath] = useState<string | null>(null)
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [dragging, setDragging] = useState(false)
  const picker = useRef<HTMLInputElement>(null)
  const listing = useQuery({ queryKey: filesKey(workspaceId, siteId, path), queryFn: () => api.files.list(workspaceId, siteId, path) })
  const entries = useMemo(() => sortEntries(listing.data ?? []), [listing.data])
  const target = [...sites, ...quick].find((connection) => connection.id === siteId)
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

  const download = (entry: RemoteEntry): void => startDownload(api.files.downloadUrl(workspaceId, siteId, entry.path, uuid()))

  const drop = (event: DragEvent<HTMLElement>): void => {
    event.preventDefault()
    setDragging(false)
    void uploads.upload(Array.from(event.dataTransfer.files))
  }

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
        setDragging(true)
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={drop}
      className="relative flex min-h-0 flex-1 flex-col"
    >
      {dragging ? (
        <div className="pointer-events-none absolute inset-2 z-20 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-background/80 text-sm font-medium">
          {t(($) => $.upload.dropHint)}
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
        <Button variant="ghost" size="sm" disabled={!listing.isSuccess} onClick={() => (picker.current as HTMLInputElement).click()}>
          <Upload />
          {t(($) => $.toolbar.upload)}
        </Button>
        <Button variant="ghost" size="sm" disabled={!listing.isSuccess} onClick={() => setDialog({ kind: 'newFolder' })}>
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
      </div>
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
      {listing.isPending && <p className="p-4 text-sm text-muted-foreground">{t(($) => $.loading)}</p>}
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
      {listing.isSuccess && entries.length === 0 && <p className="p-4 text-sm text-muted-foreground">{t(($) => $.emptyDir)}</p>}
      {listing.isSuccess && entries.length > 0 && (
        <div className="flex min-h-0 flex-1">
          <EntryTable
            key={path}
            entries={entries}
            locale={i18n.language}
            timeZone={timeZone}
            onOpen={open}
            onUp={() => onNavigate(parentPath(path))}
            selectedPath={selectedPath}
            onSelect={(entry) => setSelectedPath(entry.path)}
            onDownload={download}
            onRename={(entry) => setDialog({ kind: 'rename', entry })}
            onDelete={(entry) => setDialog({ kind: 'delete', entry })}
            onNewFolder={() => setDialog({ kind: 'newFolder' })}
          />
          <Inspector
            entry={entries[resolveSelection(entries, selectedPath)]}
            locale={i18n.language}
            timeZone={timeZone}
            onDownload={download}
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
