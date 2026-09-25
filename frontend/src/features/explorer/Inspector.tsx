import { Download, Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import type { RemoteEntry } from '@/lib/api'
import { formatInstant } from '@/lib/datetime'

import { formatSize } from './entries'

/** Props of {@link Inspector}. */
export interface InspectorProps {
  /** The selected entry. */
  entry: RemoteEntry
  /** Locale used to format the date. */
  locale: string
  /** IANA time zone the date is shown in. */
  timeZone: string
  /** Called to download the entry when it is a file. */
  onDownload: (entry: RemoteEntry) => void
  /** Called to rename the entry. */
  onRename: (entry: RemoteEntry) => void
  /** Called to delete the entry. */
  onDelete: (entry: RemoteEntry) => void
}

/** Side panel with the details of the selected entry and its actions. */
export function Inspector({ entry, locale, timeZone, onDownload, onRename, onDelete }: InspectorProps) {
  const { t } = useTranslation('explorer')

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
        <dd className="font-mono">{entry.mode}</dd>
      </dl>
      <div className="flex flex-wrap gap-2">
        {entry.type === 'file' ? (
          <Button variant="outline" size="sm" onClick={() => onDownload(entry)}>
            <Download />
            {t(($) => $.toolbar.download)}
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
