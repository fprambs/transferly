import { useQueryClient } from '@tanstack/react-query'
import { Ellipsis, KeyRound, Plus, Search, Users, UsersRound } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTable, Td, Th, Tr } from '@/components/page/DataTable'
import { Badge, EmptyCard, Page, PageHeader } from '@/components/page/Page'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Loading } from '@/components/ui/loading'
import { formatInstant } from '@/lib/datetime'
import type { Invitation, Member, Workspace } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { useSession } from '../auth/use-session'
import { useTimeZone } from '../auth/use-time-zone'
import { useWorkspace } from '../workspace/use-workspace'

import { teamErrorKeyOf, type TeamErrorKey } from './errors'
import { InviteDialog } from './InviteDialog'
import { LeaveDialog, RemoveMemberDialog, TransferDialog } from './MemberDialogs'
import { NewTeamDialog } from './NewTeamDialog'
import { RoleBadge } from './RoleBadge'
import { canManage, canManageMembers, grantableRoles, permissionRows } from './roles'
import { TeamFailure } from './TeamMessage'
import { teamKeys, useInvitations, useMembers } from './use-teams'

/** The two tabs of the page. */
type PageTab = 'members' | 'invitations'

/** Props of the table components. */
interface TableProps {
  /** The team workspace, with the role of the signed-in user. */
  workspace: Workspace
}

/** Whether a member matches the text typed in the filter: by email or by the name of the role. */
function matches(member: Member, text: string, roleName: string): boolean {
  const needle = text.trim().toLowerCase()
  return needle === '' || member.email.toLowerCase().includes(needle) || roleName.toLowerCase().includes(needle)
}

/** What the rows of the members table can ask the page to do. */
interface MemberActions {
  /** Opens the removal dialog for a member. */
  remove: (member: Member) => void
  /** Opens the transfer dialog. */
  transfer: () => void
  /** Opens the leave dialog. */
  leave: () => void
  /** Reports a failed role change. */
  fail: (failure: TeamErrorKey) => void
}

/** Props of {@link MemberRow}. */
interface MemberRowProps extends TableProps {
  /** The member to draw. */
  member: Member
  /** Identifier of the signed-in user, while it is known. */
  me: string | undefined
  /** What the row can ask the page to do. */
  actions: MemberActions
}

/** One row of the members table: who, role (editable when the user outranks them), key state, join date and actions. */
function MemberRow({ workspace, member, me, actions }: MemberRowProps) {
  const { t, i18n } = useTranslation('teams')
  const api = useApi()
  const queryClient = useQueryClient()
  const timeZone = useTimeZone()
  const isMe = member.userId === me
  const manageable = !isMe && canManage(workspace.role, member.role)
  const ownerMenu = workspace.role === 'owner' && !isMe
  const hasMenu = manageable || ownerMenu || (isMe && workspace.role !== 'owner')

  return (
    <Tr>
      <Td>
        <div className="flex min-w-0 items-center gap-2.5">
          <span aria-hidden="true" className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/12 text-[11px] font-semibold text-accent-text">
            {member.email.slice(0, 2).toUpperCase()}
          </span>
          <span className="min-w-0 leading-tight">
            <span className="block truncate">{member.email}</span>
            {isMe && <span className="text-[11px] text-muted-foreground">{t(($) => $.member.you)}</span>}
          </span>
        </div>
      </Td>
      <Td>
        {manageable ? (
          <Select
            aria-label={t(($) => $.member.roleOf, { email: member.email })}
            value={member.role}
            className="w-32"
            options={grantableRoles(workspace.role).map((role) => ({ value: role, label: t(($) => $.roles[role]) }))}
            onValueChange={async (value) => {
              try {
                await api.workspaces.setRole(workspace.id, member.userId, value as Exclude<Member['role'], 'owner'>)
                await queryClient.invalidateQueries({ queryKey: teamKeys.members(workspace.id) })
              } catch (error) {
                actions.fail(teamErrorKeyOf(error))
              }
            }}
          />
        ) : (
          <RoleBadge role={member.role} />
        )}
      </Td>
      <Td>
        <Badge tone={member.keyState === 'sealed' ? 'ok' : 'warn'} dot>
          {t(($) => $.key[member.keyState])}
        </Badge>
      </Td>
      <Td className="whitespace-nowrap text-muted-foreground">{formatInstant(member.joinedAt, { locale: i18n.language, timeZone, dateStyle: 'medium' })}</Td>
      <Td className="w-12 text-end">
        {hasMenu && (
          <DropdownMenu>
            <DropdownMenuTrigger
              aria-label={t(($) => $.member.actions, { email: member.email })}
              className="inline-flex size-6 items-center justify-center rounded text-muted-foreground hover:bg-accent data-popup-open:bg-accent"
            >
              <Ellipsis className="size-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-48">
              {manageable && <DropdownMenuItem onClick={() => actions.remove(member)}>{t(($) => $.member.remove)}</DropdownMenuItem>}
              {ownerMenu && (
                <DropdownMenuItem disabled={member.keyState !== 'sealed'} onClick={actions.transfer}>
                  {t(($) => $.member.transfer)}
                </DropdownMenuItem>
              )}
              {isMe && workspace.role !== 'owner' && <DropdownMenuItem onClick={actions.leave}>{t(($) => $.member.leave)}</DropdownMenuItem>}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </Td>
    </Tr>
  )
}

/** Props of {@link MembersTable}. */
interface MembersTableProps extends TableProps {
  /** The members to list. */
  members: Member[]
  /** Text of the filter. */
  filter: string
  /** What the rows can ask the page to do. */
  actions: MemberActions
}

/** The members of the workspace as a table with a header row. */
function MembersTable({ workspace, members, filter, actions }: MembersTableProps) {
  const { t } = useTranslation('teams')
  const session = useSession()
  const shown = members.filter((member) => matches(member, filter, t(($) => $.roles[member.role])))

  return (
    shown.length === 0 ? (
      <p className="px-5 py-8 text-center text-sm text-muted-foreground">{t(($) => $.page.empty)}</p>
    ) : (
      <DataTable aria-label={t(($) => $.page.members)}>
        <thead>
          <tr>
            <Th>{t(($) => $.table.member)}</Th>
            <Th>{t(($) => $.table.role)}</Th>
            <Th>{t(($) => $.table.key)}</Th>
            <Th>{t(($) => $.table.joined)}</Th>
            <Th>
              <span className="sr-only">{t(($) => $.page.members)}</span>
            </Th>
          </tr>
        </thead>
        <tbody>
          {shown.map((member) => (
            <MemberRow key={member.userId} workspace={workspace} member={member} me={session?.user.id} actions={actions} />
          ))}
        </tbody>
      </DataTable>
    )
  )
}

/** Props of {@link InvitationsTable}. */
interface InvitationsTableProps extends TableProps {
  /** The invitations to list. */
  invitations: Invitation[]
  /** Whether they are still loading. */
  loading: boolean
  /** Reports a failed revocation. */
  fail: (failure: TeamErrorKey) => void
}

/** The invitations waiting in the workspace, each with a button to revoke it. */
function InvitationsTable({ workspace, invitations, loading, fail }: InvitationsTableProps) {
  const { t, i18n } = useTranslation('teams')
  const api = useApi()
  const queryClient = useQueryClient()
  const timeZone = useTimeZone()

  if (loading) {
    return <Loading label={t(($) => $.invitations.loading)} className="px-5 py-4" />
  }
  if (invitations.length === 0) {
    return <p className="px-5 py-8 text-center text-sm text-muted-foreground">{t(($) => $.invitations.empty)}</p>
  }
  return (
    <DataTable aria-label={t(($) => $.page.invitations)}>
      <tbody>
        {invitations.map((invitation) => (
          <Tr key={invitation.id}>
            <Td className="max-w-64 truncate">{invitation.email}</Td>
            <Td>
              <RoleBadge role={invitation.role} />
            </Td>
            <Td className="max-w-56 truncate text-muted-foreground">{invitation.invitedBy === undefined ? '' : t(($) => $.invitations.by) + ' ' + invitation.invitedBy}</Td>
            <Td className="whitespace-nowrap text-muted-foreground">
              {t(($) => $.invitations.expires)} {formatInstant(invitation.expiresAt, { locale: i18n.language, timeZone, dateStyle: 'medium' })}
            </Td>
            <Td className="text-end">
              <Button
                variant="outline"
                size="sm"
                aria-label={t(($) => $.invitations.revokeOf, { email: invitation.email })}
                onClick={async () => {
                  try {
                    await api.invitations.revoke(workspace.id, invitation.id)
                    await queryClient.invalidateQueries({ queryKey: teamKeys.invitations(workspace.id) })
                  } catch (error) {
                    fail(teamErrorKeyOf(error))
                  }
                }}
              >
                {t(($) => $.invitations.revoke)}
              </Button>
            </Td>
          </Tr>
        ))}
      </tbody>
    </DataTable>
  )
}

/** The table of what each role can do, and the note about keys. */
function Explainers() {
  const { t } = useTranslation('teams')
  const columns = ['viewer', 'editor', 'admin', 'owner'] as const

  return (
    <div className="grid gap-3.5 md:grid-cols-2">
      <section aria-labelledby="matrix-title" className="rounded-xl border bg-card px-5 py-4 shadow-xs">
        <h2 id="matrix-title" className="mb-3 text-sm font-semibold">
          {t(($) => $.matrix.title)}
        </h2>
        <table className="w-full text-xs">
          <thead>
            <tr className="text-muted-foreground">
              <th scope="col" className="pb-1.5 text-start font-normal">
                {t(($) => $.matrix.action)}
              </th>
              {columns.map((role) => (
                <th key={role} scope="col" className="pb-1.5 text-center font-normal">
                  {t(($) => $.roles[role])}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {permissionRows.map((row) => (
              <tr key={row.action}>
                <th scope="row" className="py-1 text-start font-normal text-muted-foreground">
                  {t(($) => $.matrix[row.action])}
                </th>
                {columns.map((role) => (
                  <td key={role} className="py-1 text-center text-ok">
                    {row.allowed.includes(role) ? '✓' : ''}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      <section className="rounded-xl border border-warn/30 bg-warn/8 px-5 py-4 text-sm leading-relaxed">
        <h2 className="mb-1.5 flex items-center gap-2 font-semibold text-warn">
          <KeyRound aria-hidden="true" className="size-4" />
          {t(($) => $.keysNote.title)}
        </h2>
        {t(($) => $.keysNote.body)}
      </section>
    </div>
  )
}

/** A dialog the page can have open. */
type Open = { kind: 'invite' } | { kind: 'remove'; member: Member } | { kind: 'transfer' } | { kind: 'leave' } | null

/** Body of the page for a team workspace. */
function TeamMembers({ workspace }: TableProps) {
  const { t } = useTranslation('teams')
  const manages = canManageMembers(workspace.role)
  const members = useMembers(workspace.id, true)
  const invitations = useInvitations(workspace.id, manages)
  const [tab, setTab] = useState<PageTab>('members')
  const [filter, setFilter] = useState('')
  const [open, setOpen] = useState<Open>(null)
  const [failure, setFailure] = useState<TeamErrorKey | null>(null)
  const close = () => setOpen(null)
  const candidates = members.items.filter((member) => member.role !== 'owner' && member.keyState === 'sealed')

  return (
    <Page>
      <PageHeader
        icon={Users}
        title={t(($) => $.page.title)}
        description={t(($) => $.page.subtitle, { name: workspace.name })}
        actions={
          manages && (
            <Button onClick={() => setOpen({ kind: 'invite' })}>
              <Plus className="size-4" />
              {t(($) => $.page.invite)}
            </Button>
          )
        }
      />
      <TeamFailure failure={failure} />
      <div className="overflow-hidden rounded-xl border bg-card shadow-xs">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-2.5">
          <Tabs value={tab} onValueChange={(value) => setTab(value as PageTab)}>
            <TabsList variant="line" className="justify-start">
              <TabsTrigger value="members" className="flex-none px-3">
                {t(($) => $.page.members)} <span className="text-muted-foreground">{members.items.length}</span>
              </TabsTrigger>
              {manages && (
                <TabsTrigger value="invitations" className="flex-none px-3">
                  {t(($) => $.page.invitations)} <span className="text-muted-foreground">{invitations.items.length}</span>
                </TabsTrigger>
              )}
            </TabsList>
          </Tabs>
          {tab === 'members' && (
            <div className="relative">
              <Search aria-hidden="true" className="pointer-events-none absolute start-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input aria-label={t(($) => $.page.filter)} placeholder={t(($) => $.page.filter)} value={filter} onChange={(event) => setFilter(event.target.value)} className="h-8 w-64 ps-8" />
            </div>
          )}
        </div>
        {tab === 'members' ? (
          members.loading ? (
            <Loading label={t(($) => $.page.loading)} className="px-5 py-4" />
          ) : (
            <MembersTable
              workspace={workspace}
              members={members.items}
              filter={filter}
              actions={{
                remove: (member) => setOpen({ kind: 'remove', member }),
                transfer: () => setOpen({ kind: 'transfer' }),
                leave: () => setOpen({ kind: 'leave' }),
                fail: setFailure,
              }}
            />
          )
        ) : (
          <InvitationsTable workspace={workspace} invitations={invitations.items} loading={invitations.loading} fail={setFailure} />
        )}
      </div>
      <Explainers />
      <InviteDialog workspace={open?.kind === 'invite' ? workspace : null} onClose={close} />
      <RemoveMemberDialog workspace={workspace} member={open?.kind === 'remove' ? open.member : null} onClose={close} />
      <TransferDialog workspace={workspace} candidates={open?.kind === 'transfer' ? candidates : null} onClose={close} />
      <LeaveDialog workspace={open?.kind === 'leave' ? workspace : null} onClose={close} />
    </Page>
  )
}

/** The members page of the workspace in use: who belongs to it, with what role, and their invitations. */
export function MembersPage() {
  const { t } = useTranslation('teams')
  const workspace = useWorkspace()
  const [creating, setCreating] = useState(false)

  if (workspace === undefined) {
    return <Loading label={t(($) => $.page.loading)} className="p-7" />
  }
  if (workspace.kind === 'personal') {
    return (
      <Page>
        <PageHeader icon={Users} title={t(($) => $.page.title)} description={t(($) => $.page.subtitle, { name: workspace.name })} />
        <EmptyCard icon={UsersRound} title={t(($) => $.page.personalTitle)} body={t(($) => $.page.personalBody)}>
          <Button onClick={() => setCreating(true)}>
            <Plus className="size-4" />
            {t(($) => $.page.createTeam)}
          </Button>
        </EmptyCard>
        <NewTeamDialog open={creating} onClose={() => setCreating(false)} />
      </Page>
    )
  }
  return <TeamMembers workspace={workspace} />
}
