import { useQueryClient } from '@tanstack/react-query'
import { Ellipsis } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

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
    <li className="grid grid-cols-[2.6fr_1.2fr_1fr_1fr_1.1fr_0.4fr] items-center border-b px-4 py-2.5 text-[13px] last:border-b-0">
      <div className="flex min-w-0 items-center gap-2.5">
        <span aria-hidden="true" className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-[11px] font-semibold">
          {user.email.slice(0, 2).toUpperCase()}
        </span>
        <span className="min-w-0 leading-tight">
          <span className="block truncate">{user.email}</span>
          {isMe && <span className="text-[11px] text-muted-foreground">{t(($) => $.accounts.you)}</span>}
        </span>
      </div>
      <div>
        <span
          className={cn(
            'inline-flex h-[22px] items-center rounded-full px-2.5 text-xs font-medium',
            user.role === 'admin' ? 'bg-violet-500/15 text-violet-800 dark:text-violet-300' : 'bg-muted text-muted-foreground',
          )}
        >
          {t(($) => $.accounts.roles[user.role])}
        </span>
      </div>
      <div className="flex items-center gap-2">
        <span aria-hidden="true" className={user.disabled ? 'size-2 rounded-full bg-red-500' : 'size-2 rounded-full bg-emerald-500'} />
        {user.disabled ? t(($) => $.accounts.state.disabled) : t(($) => $.accounts.state.active)}
      </div>
      <div className="text-muted-foreground">{user.secondFactor ? t(($) => $.accounts.factor.on) : t(($) => $.accounts.factor.off)}</div>
      <div className="text-muted-foreground">{formatInstant(user.createdAt, { locale: i18n.language, timeZone, dateStyle: 'medium' })}</div>
      <div className="text-end">
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
      </div>
    </li>
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

  return (
    <div className="flex flex-col gap-4">
      <Input aria-label={t(($) => $.accounts.filter)} placeholder={t(($) => $.accounts.filter)} value={filter} onChange={(event) => setFilter(event.target.value)} className="max-w-72" />
      <AdminFailure failure={failure} />
      {users.loading ? (
        <Loading label={t(($) => $.accounts.loading)} />
      ) : users.data === undefined ? (
        <RetryFailure message={t(($) => $.errors[adminErrorKeyOf(users.error)])} retryLabel={t(($) => $.retry)} onRetry={users.refetch} />
      ) : (
        <div className="overflow-hidden rounded-lg border bg-card">
          <div className="grid grid-cols-[2.6fr_1.2fr_1fr_1fr_1.1fr_0.4fr] border-b px-4 py-2 text-[11px] tracking-wider text-muted-foreground uppercase" aria-hidden="true">
            <span>{t(($) => $.accounts.table.account)}</span>
            <span>{t(($) => $.accounts.table.role)}</span>
            <span>{t(($) => $.accounts.table.state)}</span>
            <span>{t(($) => $.accounts.table.secondFactor)}</span>
            <span>{t(($) => $.accounts.table.created)}</span>
            <span />
          </div>
          {shown.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">{t(($) => $.accounts.empty)}</p>
          ) : (
            <ul aria-label={t(($) => $.accounts.title)}>
              {shown.map((user) => (
                <AccountRow key={user.id} user={user} me={session?.user.id} ask={setPending} fail={setFailure} />
              ))}
            </ul>
          )}
        </div>
      )}
      {pending !== null && <PendingDialog pending={pending} onClose={() => setPending(null)} />}
    </div>
  )
}
