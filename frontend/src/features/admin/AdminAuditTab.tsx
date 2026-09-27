import { useInfiniteQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { formatInstant } from '@/lib/datetime'
import type { AuditEvent } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useTimeZone } from '../auth/use-time-zone'

import { adminActionKey, adminCategories, adminPageSize } from './admin-actions'
import { adminKeys } from './use-admin'

/** One line of the trail: when, who, what happened and from which address. */
function EventRow({ event }: { event: AuditEvent }) {
  const { t, i18n } = useTranslation('admin')
  const timeZone = useTimeZone()
  const key = adminActionKey(event.action)
  const role = event.meta.role === 'admin' ? t(($) => $.accounts.roles.admin) : t(($) => $.accounts.roles.user)

  return (
    <li className="grid grid-cols-[1.3fr_1.6fr_3fr_1fr] items-center gap-3 border-b px-4 py-2.5 text-[13px] last:border-b-0">
      <time dateTime={event.at} className="text-muted-foreground">
        {formatInstant(event.at, { locale: i18n.language, timeZone, dateStyle: 'medium', timeStyle: 'short' })}
      </time>
      <span className="truncate">{event.actor ?? <span className="text-muted-foreground">{t(($) => $.audit.deletedAccount)}</span>}</span>
      <span className="min-w-0 break-words">{t(($) => $.audit.actions[key], { target: event.target, action: event.action, role, count: Number(event.meta.count ?? 0) })}</span>
      <span className="truncate font-mono text-xs text-muted-foreground">{event.ip}</span>
    </li>
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
    <div className="flex flex-col gap-4">
      <label className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">{t(($) => $.audit.filter)}</span>
        <NativeSelect value={prefix} containerClassName="w-56" onChange={(event) => setPrefix(event.target.value)}>
          {adminCategories.map((entry) => (
            <option key={entry.key} value={entry.prefix}>
              {t(($) => $.audit.categories[entry.key])}
            </option>
          ))}
        </NativeSelect>
      </label>
      {trail.isPending ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.audit.loading)}</p>
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
              <EventRow key={event.id} event={event} />
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
