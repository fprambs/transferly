import { useQueryClient } from '@tanstack/react-query'
import { Ellipsis, KeyRound, Search, ShieldCheck, ShieldOff, Users, UserX } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTable, Td, Th, Tr } from '@/components/page/DataTable'
import { Badge, Section, StatTile } from '@/components/page/Page'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Loading } from '@/components/ui/loading'
import { RetryFailure } from '@/components/ui/retry-failure'
import { formatInstant } from '@/lib/datetime'
import type { AdminUser, AdminUserPatch } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'

import { useSession } from '../auth/use-session'
import { useTimeZone } from '../auth/use-time-zone'

import { AdminFailure } from './AdminMessage'
import { ConfirmDialog } from './ConfirmDialog'
import { adminErrorKeyOf, type AdminErrorKey } from './errors'
import { adminKeys, useAdminUsers } from './use-admin'

/** A change that waits for the administrator to confirm it. */
type Pending = { kind: 'disable' | 'reset'; user: AdminUser } | null

/** Whether an account matches the text typed in the filter, by email. */
function matches(user: AdminUser, text: string): boolean {
  const needle = text.trim().toLowerCase()
  return needle === '' || user.email.toLowerCase().includes(needle)
}

/** Props of {@link AccountRow}. */
interface AccountRowProps {
  /** The account to draw. */
  user: AdminUser
  /** Identifier of the signed-in account, while it is known. */
  me: string | undefined
  /** Asks to confirm disabling the account or removing its second step. */
  ask: (pending: Pending) => void
  /** Reports a failed change. */
  fail: (failure: AdminErrorKey) => void
}

/** One row: who, role, state, whether the second step is on, when it was created, and the actions of the other accounts. */
function AccountRow({ user, me, ask, fail }: AccountRowProps) {
  const { t, i18n } = useTranslation('admin')
  const api = useApi()
  const queryClient = useQueryClient()
  const timeZone = useTimeZone()
  const isMe = user.id === me
  const change = async (changes: AdminUserPatch) => {
    try {
      await api.admin.updateUser(user.id, changes)
      await queryClient.invalidateQueries({ queryKey: adminKeys.users })
    } catch (error) {
      fail(adminErrorKeyOf(error))
    }
  }

  return (
    <Tr>
      <Td>
        <div className="flex min-w-0 items-center gap-2.5">
          <span aria-hidden="true" className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/12 text-[11px] font-semibold text-accent-text">
            {user.email.slice(0, 2).toUpperCase()}
          </span>
          <span className="min-w-0 leading-tight">
            <span className="block truncate">{user.email}</span>
            {isMe && <span className="text-[11px] text-muted-foreground">{t(($) => $.accounts.you)}</span>}
          </span>
        </div>
      </Td>
      <Td>
        <Badge tone={user.role === 'admin' ? 'accent' : 'muted'}>{t(($) => $.accounts.roles[user.role])}</Badge>
      </Td>
      <Td>
        <Badge tone={user.disabled ? 'danger' : 'ok'} dot>
          {user.disabled ? t(($) => $.accounts.state.disabled) : t(($) => $.accounts.state.active)}
        </Badge>
      </Td>
      <Td>
        <span className={cn('inline-flex items-center gap-1.5', user.secondFactor ? 'text-ok' : 'text-muted-foreground')}>
          {user.secondFactor ? <ShieldCheck aria-hidden="true" className="size-3.5" /> : <ShieldOff aria-hidden="true" className="size-3.5" />}
          {user.secondFactor ? t(($) => $.accounts.factor.on) : t(($) => $.accounts.factor.off)}
        </span>
      </Td>
      <Td className="whitespace-nowrap text-muted-foreground">{formatInstant(user.createdAt, { locale: i18n.language, timeZone, dateStyle: 'medium' })}</Td>
      <Td className="w-12 text-end">
        {!isMe && (
          <DropdownMenu>
            <DropdownMenuTrigger
              aria-label={t(($) => $.accounts.actions, { email: user.email })}
              className="inline-flex size-6 items-center justify-center rounded text-muted-foreground hover:bg-accent data-popup-open:bg-accent"
            >
              <Ellipsis className="size-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-52">
              {user.disabled ? (
                <DropdownMenuItem onClick={() => void change({ disabled: false })}>{t(($) => $.accounts.enable)}</DropdownMenuItem>
              ) : (
                <DropdownMenuItem onClick={() => ask({ kind: 'disable', user })}>{t(($) => $.accounts.disable)}</DropdownMenuItem>
              )}
              <DropdownMenuItem onClick={() => void change({ role: user.role === 'admin' ? 'user' : 'admin' })}>
                {user.role === 'admin' ? t(($) => $.accounts.makeUser) : t(($) => $.accounts.makeAdmin)}
              </DropdownMenuItem>
              <DropdownMenuItem disabled={!user.secondFactor} onClick={() => ask({ kind: 'reset', user })}>
                {t(($) => $.accounts.reset)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </Td>
    </Tr>
  )
}

/** Props of {@link PendingDialog}. */
interface PendingDialogProps {
  /** The change that waits for the administrator. */
  pending: NonNullable<Pending>
  /** Called when the dialog is dismissed or the change is done. */
  onClose: () => void
}

/** The dialog that confirms the pending change and does it. */
function PendingDialog({ pending, onClose }: PendingDialogProps) {
  const { t } = useTranslation('admin')
  const api = useApi()
  const queryClient = useQueryClient()
  const email = pending.user.email
  const disabling = pending.kind === 'disable'

  return (
    <ConfirmDialog
      open
      title={disabling ? t(($) => $.disable.title, { email }) : t(($) => $.reset.title, { email })}
      body={disabling ? t(($) => $.disable.body) : t(($) => $.reset.body)}
      confirm={disabling ? t(($) => $.disable.confirm) : t(($) => $.reset.confirm)}
      onClose={onClose}
      onConfirm={async () => {
        if (pending.kind === 'disable') {
          await api.admin.updateUser(pending.user.id, { disabled: true })
        } else {
          await api.admin.resetSecondFactor(pending.user.id)
        }
        await queryClient.invalidateQueries({ queryKey: adminKeys.users })
      }}
    />
  )
}

/** The accounts of the instance with a filter and the actions an administrator has on the other accounts. */
export function AccountsTab() {
  const { t } = useTranslation('admin')
  const session = useSession()
  const users = useAdminUsers()
  const [filter, setFilter] = useState('')
  const [pending, setPending] = useState<Pending>(null)
  const [failure, setFailure] = useState<AdminErrorKey | null>(null)
  const shown = (users.data ?? []).filter((user) => matches(user, filter))

  const all = users.data ?? []

  return (
    <div className="flex flex-col gap-4">
      {users.data !== undefined && (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <StatTile icon={Users} label={t(($) => $.accounts.stats.total)} value={all.length} />
          <StatTile icon={ShieldCheck} tone="info" label={t(($) => $.accounts.stats.admins)} value={all.filter((user) => user.role === 'admin').length} />
          <StatTile icon={KeyRound} tone="ok" label={t(($) => $.accounts.stats.secondFactor)} value={all.filter((user) => user.secondFactor).length} />
          <StatTile icon={UserX} tone="danger" label={t(($) => $.accounts.stats.disabled)} value={all.filter((user) => user.disabled).length} />
        </div>
      )}
      <AdminFailure failure={failure} />
      <Section
        id="accounts-title"
        title={t(($) => $.accounts.title)}
        flush
        actions={
          <>
            {users.data !== undefined && <span className="text-xs text-muted-foreground">{t(($) => $.accounts.count, { count: shown.length })}</span>}
            <div className="relative">
              <Search aria-hidden="true" className="pointer-events-none absolute start-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input aria-label={t(($) => $.accounts.filter)} placeholder={t(($) => $.accounts.filter)} value={filter} onChange={(event) => setFilter(event.target.value)} className="h-8 w-60 ps-8" />
            </div>
          </>
        }
      >
        {users.loading ? (
          <Loading label={t(($) => $.accounts.loading)} className="px-5 py-4" />
        ) : users.data === undefined ? (
          <div className="px-5 py-4">
            <RetryFailure message={t(($) => $.errors[adminErrorKeyOf(users.error)])} retryLabel={t(($) => $.retry)} onRetry={users.refetch} />
          </div>
        ) : shown.length === 0 ? (
          <p className="px-5 py-8 text-center text-sm text-muted-foreground">{t(($) => $.accounts.empty)}</p>
        ) : (
          <DataTable aria-labelledby="accounts-title">
            <thead>
              <tr>
                <Th>{t(($) => $.accounts.table.account)}</Th>
                <Th>{t(($) => $.accounts.table.role)}</Th>
                <Th>{t(($) => $.accounts.table.state)}</Th>
                <Th>{t(($) => $.accounts.table.secondFactor)}</Th>
                <Th>{t(($) => $.accounts.table.created)}</Th>
                <Th>
                  <span className="sr-only">{t(($) => $.accounts.title)}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {shown.map((user) => (
                <AccountRow key={user.id} user={user} me={session?.user.id} ask={setPending} fail={setFailure} />
              ))}
            </tbody>
          </DataTable>
        )}
      </Section>
      {pending !== null && <PendingDialog pending={pending} onClose={() => setPending(null)} />}
    </div>
  )
}
