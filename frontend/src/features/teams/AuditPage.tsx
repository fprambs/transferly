import { useInfiniteQuery } from '@tanstack/react-query'
import { Lock, ScrollText } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTable, Td, Th, Tr } from '@/components/page/DataTable'
import { EmptyCard, Page, PageHeader, Section } from '@/components/page/Page'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
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
    <Tr>
      <Td className="whitespace-nowrap text-muted-foreground">
        <time dateTime={event.at}>{formatInstant(event.at, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })}</time>
      </Td>
      <Td className="max-w-56 truncate">{event.actor ?? <span className="text-muted-foreground">{t(($) => $.audit.deletedAccount)}</span>}</Td>
      <Td className="min-w-0 break-words">
        {t(($) => $.audit.actions[key], { target: event.target, action: event.action, role: t(($) => $.roles[(event.meta.role ?? 'viewer') as 'owner' | 'admin' | 'editor' | 'viewer']), version: event.meta.version, sites: event.meta.sites, path: event.meta.path })}
      </Td>
      <Td className="font-mono text-xs whitespace-nowrap text-muted-foreground">{event.ip}</Td>
    </Tr>
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
    <Section
      id="team-audit-events"
      title={t(($) => $.audit.events)}
      flush
      actions={
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{t(($) => $.audit.filter)}</span>
          <Select
            value={prefix}
            className="w-56"
            options={auditCategories.map((entry) => ({ value: entry.prefix, label: t(($) => $.audit.categories[entry.key]) }))}
            onValueChange={setPrefix}
          />
        </label>
      }
      footer={
        trail.hasNextPage ? (
          <Button variant="outline" disabled={trail.isFetchingNextPage} onClick={() => void trail.fetchNextPage()}>
            {t(($) => $.audit.more)}
          </Button>
        ) : undefined
      }
    >
      {trail.isPending ? (
        <Loading label={t(($) => $.audit.loading)} className="px-5 py-4" />
      ) : events.length === 0 ? (
        <div className="flex flex-col items-center gap-3 px-5 py-12 text-center">
          <span className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
            <ScrollText aria-hidden="true" className="size-5" />
          </span>
          <p className="text-sm text-muted-foreground">{t(($) => $.audit.empty)}</p>
        </div>
      ) : (
        <DataTable aria-label={t(($) => $.audit.title)}>
          <thead>
            <tr>
              <Th>{t(($) => $.audit.time)}</Th>
              <Th>{t(($) => $.audit.actor)}</Th>
              <Th>{t(($) => $.audit.what)}</Th>
              <Th>{t(($) => $.audit.ip)}</Th>
            </tr>
          </thead>
          <tbody>
            {events.map((event) => (
              <AuditEventRow key={event.id} event={event} />
            ))}
          </tbody>
        </DataTable>
      )}
    </Section>
  )
}

/** The audit log page of the workspace in use: for the admins and the owner of a team, a trail of what changed and who did it. */
export function AuditPage() {
  const { t } = useTranslation('teams')
  const workspace = useWorkspace()

  if (workspace === undefined) {
    return <Loading label={t(($) => $.audit.loading)} className="p-7" />
  }
  const header = <PageHeader icon={ScrollText} title={t(($) => $.audit.title)} description={t(($) => $.audit.subtitle, { name: workspace.name })} />
  if (workspace.kind === 'personal') {
    return (
      <Page>
        {header}
        <EmptyCard icon={ScrollText} title={t(($) => $.audit.personal)} />
      </Page>
    )
  }
  if (!canManageMembers(workspace.role)) {
    return (
      <Page>
        {header}
        <EmptyCard icon={Lock} title={t(($) => $.audit.noAccess)} />
      </Page>
    )
  }
  return (
    <Page>
      {header}
      <AuditList workspace={workspace} />
    </Page>
  )
}
