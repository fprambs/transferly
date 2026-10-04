import { Copy, Download, Eye, FilePen, Files, Hash, Pencil, Trash2, type LucideIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Loading } from '@/components/ui/loading'
import { Tip } from '@/components/ui/tip'
import type { Checksum, ChecksumAlgorithm, RemoteEntry } from '@/lib/api'
import { formatInstant, isUnknownInstant } from '@/lib/datetime'

import { formatSize } from './entries'
import { fileTypeOf } from './file-types'
import { FileTypeIcon } from './FileTypeIcon'
import { useDescribeType } from './use-file-type'

/** Props of {@link Inspector}. */
export interface InspectorProps {
  /** The selected entry. */
  entry: RemoteEntry
  /** Whether the server reports permissions; FTP does not, and the field shows a dash. */
  showPermissions: boolean
  /** Locale used to format the date. */
  locale: string
  /** IANA time zone the date is shown in. */
  timeZone: string
  /** Called to download the entry: a file as it is, a folder as a ZIP. */
  onDownload: (entry: RemoteEntry) => void
  /** Called to download the files of the entry one by one when it is a folder. */
  onDownloadEach: (entry: RemoteEntry) => void
  /** Called to compute the checksum of the entry when it is a file. */
  onChecksum: (entry: RemoteEntry, algorithm: ChecksumAlgorithm) => Promise<Checksum>
  /** Called to show the entry in the viewer when it is a file; the button is left out without it. */
  onPreview?: (entry: RemoteEntry) => void
  /** Called to open the entry in the remote editor when it is a file; the button is left out without it. */
  onEdit?: (entry: RemoteEntry) => void
  /** Called to rename the entry. */
  onRename: (entry: RemoteEntry) => void
  /** Called to delete the entry. */
  onDelete: (entry: RemoteEntry) => void
}

/** Props of {@link IconAction}. */
interface IconActionProps {
  /** Name of the action, read aloud and shown as a tooltip. */
  label: string
  /** Icon of the action. */
  icon: LucideIcon
  /** Whether the action destroys something, which draws it in red. */
  destructive?: boolean
  /** Runs the action. */
  onClick: () => void
}

/** A secondary action of the panel as an icon button with its name as a tooltip. */
function IconAction({ label, icon: Icon, destructive = false, onClick }: IconActionProps) {
  return (
    <Tip label={label}>
      <Button variant={destructive ? 'destructive' : 'outline'} size="icon-sm" aria-label={label} onClick={onClick}>
        <Icon />
      </Button>
    </Tip>
  )
}

/**
 * Side panel with the selected entry: its type icon, name, kind and size at the top, its path, date and permissions below, the main actions
 * (preview and download) as buttons and the others (edit, one by one, rename, delete) as icons, and the checksum of a file.
 */
export function Inspector({ entry, showPermissions, locale, timeZone, onDownload, onDownloadEach, onChecksum, onPreview, onEdit, onRename, onDelete }: InspectorProps) {
  const { t } = useTranslation('explorer')
  const describeType = useDescribeType()
  const [algorithm, setAlgorithm] = useState<ChecksumAlgorithm>('sha256')
  const [result, setResult] = useState<{ path: string; algorithm: ChecksumAlgorithm; state: 'working' | 'done' | 'failed'; sum?: string } | null>(null)
  const checksum = result?.path === entry.path && result.algorithm === algorithm ? result : null
  const [copied, setCopied] = useState(false)

  return (
    <aside aria-label={t(($) => $.inspector.label)} className="flex h-full w-full flex-col gap-4 overflow-y-auto bg-card p-4">
      <div className="flex items-start gap-3">
        <span className="flex size-11 shrink-0 items-center justify-center rounded-xl border bg-background">
          <FileTypeIcon type={fileTypeOf(entry)} className="size-5" />
        </span>
        <div className="flex min-w-0 flex-col gap-0.5">
          <p className="font-medium break-all">{entry.name}</p>
          <p className="text-xs text-muted-foreground">
            {describeType(entry)}
            {entry.type === 'directory' ? null : ` · ${formatSize(entry.size)}`}
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {entry.type === 'file' && onPreview ? (
          <Button size="sm" onClick={() => onPreview(entry)}>
            <Eye />
            {t(($) => $.menu.preview)}
          </Button>
        ) : null}
        {entry.type === 'file' || entry.type === 'directory' ? (
          <Button variant={entry.type === 'file' && onPreview ? 'outline' : 'default'} size="sm" onClick={() => onDownload(entry)}>
            <Download />
            {entry.type === 'file' ? t(($) => $.toolbar.download) : t(($) => $.toolbar.downloadZip)}
          </Button>
        ) : null}
        <span className="flex items-center gap-1">
          {entry.type === 'file' && onEdit ? <IconAction label={t(($) => $.toolbar.edit)} icon={FilePen} onClick={() => onEdit(entry)} /> : null}
          {entry.type === 'directory' ? <IconAction label={t(($) => $.menu.downloadEach)} icon={Files} onClick={() => onDownloadEach(entry)} /> : null}
          <IconAction label={t(($) => $.toolbar.rename)} icon={Pencil} onClick={() => onRename(entry)} />
          <IconAction label={t(($) => $.toolbar.delete)} icon={Trash2} destructive onClick={() => onDelete(entry)} />
        </span>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 border-t pt-4 text-xs">
        <dt className="text-muted-foreground">{t(($) => $.inspector.path)}</dt>
        <dd className="font-mono break-all">{entry.path}</dd>
        <dt className="text-muted-foreground">{t(($) => $.inspector.modified)}</dt>
        <dd>{isUnknownInstant(entry.modifiedAt) ? '—' : formatInstant(entry.modifiedAt, { locale, timeZone, dateStyle: 'medium', timeStyle: 'medium' })}</dd>
        <dt className="text-muted-foreground">{t(($) => $.inspector.mode)}</dt>
        <dd className="font-mono">{showPermissions ? entry.mode : '—'}</dd>
      </dl>
      {entry.type === 'file' ? (
        <div className="flex flex-col gap-2 border-t pt-4">
          <div className="flex items-center gap-2">
            <Select
              aria-label={t(($) => $.checksum.algorithm)}
              className="h-7 w-28 text-xs"
              value={algorithm}
              options={[
                { value: 'sha256', label: 'SHA-256' },
                { value: 'sha512', label: 'SHA-512' },
                { value: 'sha1', label: 'SHA-1' },
                { value: 'md5', label: 'MD5' },
              ]}
              onValueChange={(value) => setAlgorithm(value as ChecksumAlgorithm)}
            />
            <Button
              variant="outline"
              size="sm"
              disabled={checksum?.state === 'working'}
              onClick={() => {
                const job = { path: entry.path, algorithm }
                setResult({ ...job, state: 'working' })
                setCopied(false)
                onChecksum(entry, algorithm).then(
                  (found) => setResult({ ...job, state: 'done', sum: found.checksum }),
                  () => setResult({ ...job, state: 'failed' }),
                )
              }}
            >
              <Hash />
              {t(($) => $.checksum.compute)}
            </Button>
          </div>
          {checksum?.state === 'working' ? <Loading label={t(($) => $.checksum.working)} className="text-xs" /> : null}
          {checksum?.state === 'failed' ? <p role="alert" className="text-xs text-destructive">{t(($) => $.checksum.failed)}</p> : null}
          {checksum?.state === 'done' ? (
            <div className="flex items-start gap-2">
              <code aria-label={t(($) => $.checksum.value)} className="font-mono text-xs break-all">{checksum.sum}</code>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={copied ? t(($) => $.checksum.copied) : t(($) => $.checksum.copy)}
                onClick={async () => {
                  await navigator.clipboard.writeText(checksum.sum as string)
                  setCopied(true)
                }}
              >
                <Copy />
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
    </aside>
  )
}
