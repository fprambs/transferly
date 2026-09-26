import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import type { ActivityEntry } from '@/lib/api'
import { formatInstant } from '@/lib/datetime'
import { useApi } from '@/lib/api/use-api'

/** How often, in milliseconds, the log is read again while it is on show. */
const refreshEvery = 5000

/** Props of {@link ActivityList}. */
export interface ActivityListProps {
  /** Workspace whose log is shown. */
  workspaceId: string
  /** Locale used to format times. */
  locale: string
  /** IANA time zone times are shown in. */
  timeZone: string
}

/** Splits the detail of a transfer entry, such as `upload:done`, into its direction and its final state. */
function transferParts(detail: string | undefined): { direction: 'upload' | 'download' | 'copy'; status: 'done' | 'failed' | 'canceled' | 'interrupted' } {
  const [direction, status] = (detail ?? '').split(':')
  return {
    direction: direction === 'download' || direction === 'copy' ? direction : 'upload',
    status: status === 'failed' || status === 'canceled' || status === 'interrupted' ? status : 'done',
  }
}

/** The log of what the user did on their connections, newest first, kept up to date while it is on show. */
export function ActivityList({ workspaceId, locale, timeZone }: ActivityListProps) {
  const { t } = useTranslation('transfers')
  const api = useApi()
  const log = useQuery({
    queryKey: ['workspaces', workspaceId, 'activity'],
    queryFn: () => api.activity.list(workspaceId, { limit: 100 }),
    refetchInterval: refreshEvery,
  })

  if (log.isError) {
    return <p role="alert" className="px-4 py-3 text-sm text-destructive">{t(($) => $.activity.failed)}</p>
  }
  const entries = log.data ?? []
  if (log.isSuccess && entries.length === 0) {
    return <p className="px-4 py-3 text-sm text-muted-foreground">{t(($) => $.activity.empty)}</p>
  }
  return (
    <ul aria-label={t(($) => $.activity.list)}>
      {entries.map((entry) => (
        <ActivityRow key={entry.id} entry={entry} locale={locale} timeZone={timeZone} />
      ))}
    </ul>
  )
}

/** One entry of the log: when, what, where and how it went. */
function ActivityRow({ entry, locale, timeZone }: { entry: ActivityEntry; locale: string; timeZone: string }) {
  const { t } = useTranslation('transfers')
  const parts = transferParts(entry.detail)
  const what =
    entry.kind === 'transfer'
      ? t(($) => $.activity.transfer, { direction: t(($) => $[parts.direction]), status: t(($) => $.status[parts.status]) })
      : entry.kind === 'rename'
        ? t(($) => $.activity.rename, { to: entry.detail })
        : entry.kind === 'checksum'
          ? t(($) => $.activity.checksum, { algorithm: entry.detail })
          : t(($) => $.activity[entry.kind])
  return (
    <li className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-b px-4 py-2 text-[13px] last:border-b-0">
      <time dateTime={entry.at} className="font-mono text-[11px] text-muted-foreground">
        {formatInstant(entry.at, { locale, timeZone, dateStyle: 'medium', timeStyle: 'medium' })}
      </time>
      <span className="font-medium">{what}</span>
      <span className="truncate font-mono text-xs">{entry.path}</span>
      <span className="truncate text-xs text-muted-foreground">{entry.siteName}</span>
      {entry.errorCode === undefined ? null : <span className="font-mono text-xs text-destructive">{entry.errorCode}</span>}
    </li>
  )
}
