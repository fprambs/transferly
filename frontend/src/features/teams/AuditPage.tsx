import { useInfiniteQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Loading } from '@/components/ui/loading'
import { formatInstant } from '@/lib/datetime'
import type { AuditEvent, Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useTimeZone } from '../auth/use-time-zone'
import { useWorkspace } from '../workspace/use-workspace'

import { auditActionKey, auditCategories, auditPageSize } from './audit-actions'
import { canManageMembers } from './roles'

/** Props of {@link AuditEventRow}. */
interface AuditEventRowProps {
  /** The event to draw. */
  event: AuditEvent
}

/** One line of the trail: when, who, what happened and from which address. */
function AuditEventRow({ event }: AuditEventRowProps) {
  const { t, i18n } = useTranslation('teams')
  const timeZone = useTimeZone()
  const key = auditActionKey(event.action)

  return (
    <li className="grid grid-cols-[1.3fr_1.6fr_3fr_1fr] items-center gap-3 border-b px-4 py-2.5 text-[13px] last:border-b-0">
      <time dateTime={event.at} className="text-muted-foreground">
        {formatInstant(event.at, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })}
      </time>
      <span className="truncate">{event.actor ?? <span className="text-muted-foreground">{t(($) => $.audit.deletedAccount)}</span>}</span>
      <span className="min-w-0 break-words">
        {t(($) => $.audit.actions[key], { target: event.target, action: event.action, role: t(($) => $.roles[(event.meta.role ?? 'viewer') as 'owner' | 'admin' | 'editor' | 'viewer']), version: event.meta.version, sites: event.meta.sites })}
      </span>
      <span className="truncate font-mono text-xs text-muted-foreground">{event.ip}</span>
    </li>
  )
}

/** Props of {@link AuditList}. */
interface AuditListProps {
  /** The team workspace whose trail is read. */
  workspace: Workspace
}

/** The trail of a team workspace, narrowed by a group of actions and read a page at a time. */
function AuditList({ workspace }: AuditListProps) {
  const { t } = useTranslation('teams')
  const api = useApi()
  const [prefix, setPrefix] = useState('')
  const trail = useInfiniteQuery({
    queryKey: ['workspaces', workspace.id, 'audit', prefix],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => api.audit.list(workspace.id, { action: prefix === '' ? undefined : prefix, before: pageParam === 0 ? undefined : pageParam, limit: auditPageSize }),
    getNextPageParam: (last) => (last.length < auditPageSize ? undefined : last[last.length - 1].id),
  })
  const events = trail.data?.pages.flat() ?? []

  return (
    <div className="flex flex-col gap-4">
      <label className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">{t(($) => $.audit.filter)}</span>
        <NativeSelect value={prefix} containerClassName="w-56" onChange={(event) => setPrefix(event.target.value)}>
          {auditCategories.map((entry) => (
            <option key={entry.key} value={entry.prefix}>
              {t(($) => $.audit.categories[entry.key])}
            </option>
          ))}
        </NativeSelect>
      </label>
      {trail.isPending ? (
        <Loading label={t(($) => $.audit.loading)} />
      ) : events.length === 0 ? (
        <p className="rounded-lg border bg-card px-4 py-6 text-center text-sm text-muted-foreground">{t(($) => $.audit.empty)}</p>
      ) : (
        <div className="overflow-hidden rounded-lg border bg-card">
          <div className="grid grid-cols-[1.3fr_1.6fr_3fr_1fr] gap-3 border-b px-4 py-2 text-[11px] tracking-wider text-muted-foreground uppercase" aria-hidden="true">
            <span>{t(($) => $.audit.time)}</span>
            <span>{t(($) => $.audit.actor)}</span>
            <span>{t(($) => $.audit.what)}</span>
            <span>{t(($) => $.audit.ip)}</span>
          </div>
          <ul aria-label={t(($) => $.audit.title)}>
            {events.map((event) => (
              <AuditEventRow key={event.id} event={event} />
            ))}
          </ul>
        </div>
      )}
      {trail.hasNextPage && (
        <Button variant="outline" className="self-start" disabled={trail.isFetchingNextPage} onClick={() => void trail.fetchNextPage()}>
          {t(($) => $.audit.more)}
        </Button>
      )}
    </div>
  )
}

/** The audit log page of the workspace in use: for the admins and the owner of a team, a trail of what changed and who did it. */
export function AuditPage() {
  const { t } = useTranslation('teams')
  const workspace = useWorkspace()

  if (workspace === undefined) {
    return <Loading label={t(($) => $.audit.loading)} className="p-7" />
  }
  if (workspace.kind === 'personal') {
    return <p className="p-7 text-sm text-muted-foreground">{t(($) => $.audit.personal)}</p>
  }
  if (!canManageMembers(workspace.role)) {
    return <p className="p-7 text-sm text-muted-foreground">{t(($) => $.audit.noAccess)}</p>
  }
  return (
    <div className="flex flex-col gap-4 p-7">
      <header>
        <h1 className="text-xl font-semibold">{t(($) => $.audit.title)}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t(($) => $.audit.subtitle, { name: workspace.name })}</p>
      </header>
      <AuditList workspace={workspace} />
    </div>
  )
}
