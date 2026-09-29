/** Translation keys of the actions of the audit trail the interface words in full. */
const known = {
  'workspace.created': 'workspaceCreated',
  'workspace.ownership_transferred': 'workspaceOwnershipTransferred',
  'member.joined': 'memberJoined',
  'member.role_changed': 'memberRoleChanged',
  'member.removed': 'memberRemoved',
  'member.left': 'memberLeft',
  'invitation.created': 'invitationCreated',
  'invitation.revoked': 'invitationRevoked',
  'key.rotated': 'keyRotated',
  'site.created': 'siteCreated',
  'site.updated': 'siteUpdated',
  'site.deleted': 'siteDeleted',
  'site.credential_provided': 'siteCredentialProvided',
  'sites.imported': 'sitesImported',
  'host_key.trusted': 'hostKeyTrusted',
  'host_key.forgotten': 'hostKeyForgotten',
  'bookmark.created': 'bookmarkCreated',
  'bookmark.updated': 'bookmarkUpdated',
  'bookmark.deleted': 'bookmarkDeleted',
} as const

/** Translation key of the sentence that describes an action; an action the interface does not know gets the generic one. */
export type AuditActionKey = (typeof known)[keyof typeof known] | 'unknown'

/** Maps an action of the trail to the key of its sentence. */
export function auditActionKey(action: string): AuditActionKey {
  return (known as Readonly<Record<string, AuditActionKey>>)[action] ?? 'unknown'
}

/** The groups the trail can be narrowed to, with the action prefix that selects each. */
export const auditCategories = [
  { key: 'all', prefix: '' },
  { key: 'members', prefix: 'member.' },
  { key: 'invitations', prefix: 'invitation.' },
  { key: 'sites', prefix: 'site' },
  { key: 'keys', prefix: 'key.' },
  { key: 'servers', prefix: 'host_key.' },
  { key: 'bookmarks', prefix: 'bookmark.' },
  { key: 'workspace', prefix: 'workspace.' },
] as const

/** How many events each page of the trail holds. */
export const auditPageSize = 50
