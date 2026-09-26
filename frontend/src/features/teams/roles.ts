import type { GrantableRole, Member } from '@/lib/api'

/** A role of a member of a workspace. */
export type Role = Member['role']

/** Every role, from the most to the least powerful. */
export const roles: readonly Role[] = ['owner', 'admin', 'editor', 'viewer']

/** How much each role can do; a member can act only on members ranked below them. */
const rank: Readonly<Record<Role, number>> = { owner: 4, admin: 3, editor: 2, viewer: 1 }

/** The roles an invitation or a role change can give, from the most to the least powerful. */
const grantable: readonly GrantableRole[] = ['admin', 'editor', 'viewer']

/** Tells whether a role may manage members at all: only admins and the owner may. */
export function canManageMembers(role: Role): boolean {
  return rank[role] >= rank.admin
}

/** Tells whether a member with the role `mine` may change or remove a member with the role `theirs`. */
export function canManage(mine: Role, theirs: Role): boolean {
  return canManageMembers(mine) && rank[mine] > rank[theirs]
}

/** Lists the roles a member with the role `mine` may give: the ones ranked below their own. */
export function grantableRoles(mine: Role): GrantableRole[] {
  return grantable.filter((role) => rank[role] < rank[mine])
}

/** One row of the table of what each role can do. */
export interface PermissionRow {
  /** Key of the translated name of the action. */
  action: 'browse' | 'upload' | 'sites' | 'members' | 'workspace'
  /** The roles that may do it. */
  allowed: readonly Role[]
}

/** What each role can do, as documented in the architecture: the same matrix the server enforces. */
export const permissionRows: readonly PermissionRow[] = [
  { action: 'browse', allowed: ['viewer', 'editor', 'admin', 'owner'] },
  { action: 'upload', allowed: ['editor', 'admin', 'owner'] },
  { action: 'sites', allowed: ['editor', 'admin', 'owner'] },
  { action: 'members', allowed: ['admin', 'owner'] },
  { action: 'workspace', allowed: ['owner'] },
]
