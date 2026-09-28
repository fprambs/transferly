import { Copy, Download, Files, Hash, Pencil, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Loading } from '@/components/ui/loading'
import type { Checksum, ChecksumAlgorithm, RemoteEntry } from '@/lib/api'
import { formatInstant } from '@/lib/datetime'

import { formatSize } from './entries'

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
  /** Called to rename the entry. */
  onRename: (entry: RemoteEntry) => void
  /** Called to delete the entry. */
  onDelete: (entry: RemoteEntry) => void
}

/** Side panel with the details of the selected entry and its actions. */
export function Inspector({ entry, showPermissions, locale, timeZone, onDownload, onDownloadEach, onChecksum, onRename, onDelete }: InspectorProps) {
  const { t } = useTranslation('explorer')
  const [algorithm, setAlgorithm] = useState<ChecksumAlgorithm>('sha256')
  const [result, setResult] = useState<{ path: string; algorithm: ChecksumAlgorithm; state: 'working' | 'done' | 'failed'; sum?: string } | null>(null)
  const checksum = result?.path === entry.path && result.algorithm === algorithm ? result : null
  const [copied, setCopied] = useState(false)

  return (
    <aside aria-label={t(($) => $.inspector.label)} className="hidden w-72 shrink-0 flex-col gap-4 border-s bg-card p-4 lg:flex">
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        <dt className="text-muted-foreground">{t(($) => $.inspector.name)}</dt>
        <dd className="font-mono break-all">{entry.name}</dd>
        <dt className="text-muted-foreground">{t(($) => $.inspector.path)}</dt>
        <dd className="font-mono break-all">{entry.path}</dd>
        <dt className="text-muted-foreground">{t(($) => $.inspector.kind)}</dt>
        <dd>{t(($) => $[entry.type])}</dd>
        <dt className="text-muted-foreground">{t(($) => $.inspector.size)}</dt>
        <dd className="font-mono">{entry.type === 'directory' ? '—' : formatSize(entry.size)}</dd>
        <dt className="text-muted-foreground">{t(($) => $.inspector.modified)}</dt>
        <dd>{formatInstant(entry.modifiedAt, { locale, timeZone, dateStyle: 'medium', timeStyle: 'medium' })}</dd>
        <dt className="text-muted-foreground">{t(($) => $.inspector.mode)}</dt>
        <dd className="font-mono">{showPermissions ? entry.mode : '—'}</dd>
      </dl>
      {entry.type === 'file' ? (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <NativeSelect
              aria-label={t(($) => $.checksum.algorithm)}
              containerClassName="w-28"
              className="h-7 text-xs"
              value={algorithm}
              onChange={(event) => setAlgorithm(event.target.value as ChecksumAlgorithm)}
            >
              <option value="sha256">SHA-256</option>
              <option value="sha512">SHA-512</option>
              <option value="sha1">SHA-1</option>
              <option value="md5">MD5</option>
            </NativeSelect>
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
      <div className="flex flex-wrap gap-2">
        {entry.type === 'file' || entry.type === 'directory' ? (
          <Button variant="outline" size="sm" onClick={() => onDownload(entry)}>
            <Download />
            {entry.type === 'file' ? t(($) => $.toolbar.download) : t(($) => $.toolbar.downloadZip)}
          </Button>
        ) : null}
        {entry.type === 'directory' ? (
          <Button variant="outline" size="sm" onClick={() => onDownloadEach(entry)}>
            <Files />
            {t(($) => $.toolbar.downloadEach)}
          </Button>
        ) : null}
        <Button variant="outline" size="sm" onClick={() => onRename(entry)}>
          <Pencil />
          {t(($) => $.toolbar.rename)}
        </Button>
        <Button variant="outline" size="sm" onClick={() => onDelete(entry)}>
          <Trash2 />
          {t(($) => $.toolbar.delete)}
        </Button>
      </div>
    </aside>
  )
}
