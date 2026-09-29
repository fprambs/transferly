import { describe, expect, it } from 'vitest'

import { canManage, canManageMembers, canManageSites, grantableRoles, permissionRows, roles } from './roles'

/**
 * Covers the role rules of {@link roles}.
 *
 * Scenarios:
 * - only admins and the owner manage members, and editors and above manage sites and their bookmarks
 * - a member acts only on members ranked below them, so nobody acts on an equal, and the owner is never acted on
 * - the owner can give admin, editor and viewer, an admin editor and viewer, and an editor only viewer, and a viewer nothing
 * - the table of permissions matches the roles the server allows for each action
 */
describe('roles', () => {
  it('lets only admins and the owner manage members', () => {
    expect(roles.filter(canManageMembers)).toEqual(['owner', 'admin'])
    expect(roles.filter(canManageSites)).toEqual(['owner', 'admin', 'editor'])
  })

  it('lets a member act only on members ranked below them', () => {
    expect(canManage('owner', 'admin')).toBe(true)
    expect(canManage('admin', 'editor')).toBe(true)
    expect(canManage('admin', 'admin')).toBe(false)
    expect(canManage('admin', 'owner')).toBe(false)
    expect(canManage('editor', 'viewer')).toBe(false)
  })

  it('lists the roles a member may give', () => {
    expect(grantableRoles('owner')).toEqual(['admin', 'editor', 'viewer'])
    expect(grantableRoles('admin')).toEqual(['editor', 'viewer'])
    expect(grantableRoles('editor')).toEqual(['viewer'])
    expect(grantableRoles('viewer')).toEqual([])
  })

  it('documents who may do what', () => {
    const byAction = Object.fromEntries(permissionRows.map((row) => [row.action, row.allowed]))

    expect(byAction.browse).toEqual(['viewer', 'editor', 'admin', 'owner'])
    expect(byAction.workspace).toEqual(['owner'])
    expect(byAction.members).toEqual(['admin', 'owner'])
  })
})
