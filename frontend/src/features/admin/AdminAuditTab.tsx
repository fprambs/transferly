import { useInfiniteQuery } from '@tanstack/react-query'
import { ScrollText } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTable, Td, Th, Tr } from '@/components/page/DataTable'
import { Section } from '@/components/page/Page'
import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { Select } from '@/components/ui/select'
import { RetryFailure } from '@/components/ui/retry-failure'
import { formatInstant } from '@/lib/datetime'
import type { AuditEvent } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useTimeZone } from '../auth/use-time-zone'

import { adminActionKey, adminCategories, adminPageSize } from './admin-actions'
import { adminErrorKeyOf } from './errors'
import { adminKeys } from './use-admin'

/** One line of the trail: when, who, what happened and from which address. */
function EventRow({ event }: { event: AuditEvent }) {
  const { t, i18n } = useTranslation('admin')
  const timeZone = useTimeZone()
  const key = adminActionKey(event.action)
  const role = event.meta.role === 'admin' ? t(($) => $.accounts.roles.admin) : t(($) => $.accounts.roles.user)

  return (
    <Tr>
      <Td className="whitespace-nowrap text-muted-foreground">
        <time dateTime={event.at}>{formatInstant(event.at, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })}</time>
      </Td>
      <Td className="max-w-56 truncate">{event.actor ?? <span className="text-muted-foreground">{t(($) => $.audit.deletedAccount)}</span>}</Td>
      <Td className="min-w-0 break-words">{t(($) => $.audit.actions[key], { target: event.target, action: event.action, role, count: Number(event.meta.count ?? 0) })}</Td>
      <Td className="font-mono text-xs whitespace-nowrap text-muted-foreground">{event.ip}</Td>
    </Tr>
  )
}

/** What administrators did that belongs to no workspace, narrowed by a group of actions and read a page at a time. */
export function AdminAuditTab() {
  const { t } = useTranslation('admin')
  const api = useApi()
  const [prefix, setPrefix] = useState('')
  const trail = useInfiniteQuery({
    queryKey: adminKeys.audit(prefix),
    initialPageParam: 0,
    queryFn: ({ pageParam }) => api.admin.audit({ action: prefix === '' ? undefined : prefix, before: pageParam === 0 ? undefined : pageParam, limit: adminPageSize }),
    getNextPageParam: (last) => (last.length < adminPageSize ? undefined : last[last.length - 1].id),
  })
  const events = trail.data?.pages.flat() ?? []

  return (
    <Section
      id="admin-audit-title"
      title={t(($) => $.audit.title)}
      flush
      actions={
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{t(($) => $.audit.filter)}</span>
          <Select
            value={prefix}
            className="w-56"
            options={adminCategories.map((entry) => ({ value: entry.prefix, label: t(($) => $.audit.categories[entry.key]) }))}
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
      ) : trail.isError ? (
        <div className="px-5 py-4">
          <RetryFailure message={t(($) => $.errors[adminErrorKeyOf(trail.error)])} retryLabel={t(($) => $.retry)} onRetry={() => void trail.refetch()} />
        </div>
      ) : events.length === 0 ? (
        <div className="flex flex-col items-center gap-3 px-5 py-12 text-center">
          <span className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
            <ScrollText aria-hidden="true" className="size-5" />
          </span>
          <p className="text-sm text-muted-foreground">{t(($) => $.audit.empty)}</p>
        </div>
      ) : (
        <DataTable aria-labelledby="admin-audit-title">
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
              <EventRow key={event.id} event={event} />
            ))}
          </tbody>
        </DataTable>
      )}
    </Section>
  )
}
