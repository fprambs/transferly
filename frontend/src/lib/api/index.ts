import type { Transport } from '../transport'

import type { components } from './schema'

/** The schemas of the OpenAPI contract, generated into `schema.d.ts`. */
type Schemas = components['schemas']

/** Credentials of an account. */
export type Credentials = Schemas['Credentials']
/** The signed-in user with the anti-CSRF token of the session. */
export type SessionInfo = Schemas['SessionInfo']
/** An account. */
export type User = Schemas['User']
/** Preferences a user can change. */
export type Preferences = Schemas['Preferences']
/** State of the vault in a session. */
export type VaultStatus = Schemas['VaultStatus']
/** A workspace with the role of the signed-in user in it. */
export type Workspace = Schemas['Workspace']
/** A member of a team workspace with their role and the state of their copy of the workspace key. */
export type Member = Schemas['Member']
/** A role a member can be given. Owners come only from transferring ownership. */
export type GrantableRole = Schemas['MemberRoleInput']['role']
/** An offer to join a team workspace, as its admins see it. */
export type Invitation = Schemas['Invitation']
/** A new invitation with its secret, shown only once. */
export type InvitationCreated = Schemas['InvitationCreated']
/** What an invitation offers, for whoever holds its token. */
export type InvitationPreview = Schemas['InvitationPreview']
/** One line of the audit trail of a workspace. */
export type AuditEvent = Schemas['AuditEvent']
/** A collection that groups sites. */
export type Collection = Schemas['Collection']
/** What to change in a collection. */
export type CollectionChanges = Schemas['CollectionChanges']
/** A saved connection. */
export type Site = Schemas['Site']
/** Settings and credentials of a site. */
export type SiteInput = Schemas['SiteInput']
/** A server tried without saving it. */
export type QuickConnection = Schemas['QuickConnection']
/** Settings and credentials of a quick connection. */
export type QuickConnectInput = Schemas['QuickConnectInput']
/** A server key the workspace trusts. */
export type KnownHost = Schemas['KnownHost']
/** A file or directory on a remote server. */
export type RemoteEntry = Schemas['RemoteEntry']
/** A recorded upload or download. */
export type Transfer = Schemas['Transfer']

/** A character set of file names. */
export type Charset = Schemas['Charset']

/** What a probe learned about the time zone of a server's listings. */
export type TimeZoneProbe = Schemas['TimeZoneProbe']

/** What happened, or would happen, to the sites of an imported file. */
export type ImportResult = Schemas['ImportResult']

/** How to import a site manager file. */
export interface ImportOptions {
  /** Only report what would happen. */
  dryRun?: boolean
  /** Store the passwords of the file in the vault. */
  passwords?: boolean
  /** What to do with a site whose name is taken. */
  onConflict?: 'skip' | 'rename'
}

/** One line of the activity log. */
export type ActivityEntry = Schemas['ActivityEntry']

/** The digest of a remote file. */
export type Checksum = Schemas['Checksum']

/** Algorithms a checksum can be computed with. */
export type ChecksumAlgorithm = Checksum['algorithm']

/** What the server needs to copy a file from one site to another. */
export type CopyInput = Schemas['CopyInput']

/** Where a waiting transfer stands in the queue: -1 low, 0 normal, 1 high. */
export type TransferPriority = Schemas['TransferPriorityInput']['priority']

/** What to do when an uploaded file exists. */
export type UploadMode = 'create' | 'overwrite' | 'resume'

/** Options of an upload. */
export interface UploadOptions {
  /** What to do when the file exists; the server default is `create`. */
  mode?: UploadMode
  /** Byte to continue at, with `mode: 'resume'`. */
  offset?: number
  /** Identifier chosen by the client to follow and resume the transfer. */
  transferId?: string
  /** Expected size, for progress. */
  size?: number
  /** Aborts the upload. */
  signal?: AbortSignal
}

/** Root of every API path. */
const root = '/api/v1'

/** Returns the path of the file endpoints of a site. */
function filesPath(workspaceId: string, siteId: string): string {
  return `${root}/workspaces/${workspaceId}/sites/${siteId}/files`
}

/**
 * Builds the typed client of the backend on top of a transport.
 *
 * Every function is one endpoint of `api/openapi.yaml`; failures are thrown by the transport as `ApiError`.
 *
 * @param transport - How calls reach the backend.
 */
export function createApi(transport: Transport) {
  return {
    auth: {
      setupStatus: () => transport.request<{ required: boolean }>({ method: 'GET', path: `${root}/setup/status` }),
      setup: (credentials: Credentials) => transport.request<SessionInfo>({ method: 'POST', path: `${root}/setup`, body: credentials }),
      login: (credentials: Credentials) => transport.request<SessionInfo>({ method: 'POST', path: `${root}/auth/login`, body: credentials }),
      logout: () => transport.request<void>({ method: 'POST', path: `${root}/auth/logout` }),
      me: () => transport.request<SessionInfo>({ method: 'GET', path: `${root}/me` }),
      updateMe: (preferences: Preferences) => transport.request<User>({ method: 'PATCH', path: `${root}/me`, body: preferences }),
    },
    vault: {
      status: () => transport.request<VaultStatus>({ method: 'GET', path: `${root}/vault` }),
      setup: (masterPassword: string) =>
        transport.request<{ recoveryKit: string }>({ method: 'POST', path: `${root}/vault/setup`, body: { masterPassword } }),
      unlock: (masterPassword: string) => transport.request<void>({ method: 'POST', path: `${root}/vault/unlock`, body: { masterPassword } }),
      lock: () => transport.request<void>({ method: 'POST', path: `${root}/vault/lock` }),
      changePassword: (currentPassword: string, newPassword: string) =>
        transport.request<void>({ method: 'POST', path: `${root}/vault/password`, body: { currentPassword, newPassword } }),
      recover: (recoveryKit: string, newPassword: string) =>
        transport.request<void>({ method: 'POST', path: `${root}/vault/recover`, body: { recoveryKit, newPassword } }),
      regenerateRecoveryKit: (masterPassword: string) =>
        transport.request<{ recoveryKit: string }>({ method: 'POST', path: `${root}/vault/recovery-kit`, body: { masterPassword } }),
    },
    workspaces: {
      list: () => transport.request<Workspace[]>({ method: 'GET', path: `${root}/workspaces` }),
      get: (workspaceId: string) => transport.request<Workspace>({ method: 'GET', path: `${root}/workspaces/${workspaceId}` }),
      create: (name: string) => transport.request<Workspace>({ method: 'POST', path: `${root}/workspaces`, body: { name } }),
      members: (workspaceId: string) => transport.request<Member[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/members` }),
      setRole: (workspaceId: string, userId: string, role: GrantableRole) =>
        transport.request<void>({ method: 'PATCH', path: `${root}/workspaces/${workspaceId}/members/${userId}`, body: { role } }),
      removeMember: (workspaceId: string, userId: string) =>
        transport.request<void>({ method: 'DELETE', path: `${root}/workspaces/${workspaceId}/members/${userId}` }),
      leave: (workspaceId: string) => transport.request<void>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/leave` }),
      transfer: (workspaceId: string, userId: string) =>
        transport.request<void>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/transfer`, body: { userId } }),
    },
    audit: {
      list: (workspaceId: string, query: { action?: string; before?: number; limit?: number } = {}) =>
        transport.request<AuditEvent[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/audit`, query }),
    },
    invitations: {
      list: (workspaceId: string) => transport.request<Invitation[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/invitations` }),
      create: (workspaceId: string, input: { email: string; role: GrantableRole }) =>
        transport.request<InvitationCreated>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/invitations`, body: input }),
      revoke: (workspaceId: string, invitationId: string) =>
        transport.request<void>({ method: 'DELETE', path: `${root}/workspaces/${workspaceId}/invitations/${invitationId}` }),
      preview: (token: string) => transport.request<InvitationPreview>({ method: 'POST', path: `${root}/invitations/preview`, body: { token } }),
      accept: (token: string) => transport.request<Workspace>({ method: 'POST', path: `${root}/invitations/accept`, body: { token } }),
      join: (token: string, password: string) =>
        transport.request<SessionInfo>({ method: 'POST', path: `${root}/invitations/join`, body: { token, password } }),
    },
    collections: {
      list: (workspaceId: string) => transport.request<Collection[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/collections` }),
      create: (workspaceId: string, input: { name: string; parentId?: string }) =>
        transport.request<Collection>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/collections`, body: input }),
      update: (workspaceId: string, collectionId: string, changes: CollectionChanges) =>
        transport.request<Collection>({ method: 'PATCH', path: `${root}/workspaces/${workspaceId}/collections/${collectionId}`, body: changes }),
      remove: (workspaceId: string, collectionId: string) =>
        transport.request<void>({ method: 'DELETE', path: `${root}/workspaces/${workspaceId}/collections/${collectionId}` }),
    },
    sites: {
      list: (workspaceId: string) => transport.request<Site[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/sites` }),
      get: (workspaceId: string, siteId: string) =>
        transport.request<Site>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/sites/${siteId}` }),
      create: (workspaceId: string, input: SiteInput) =>
        transport.request<Site>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/sites`, body: input }),
      update: (workspaceId: string, siteId: string, input: SiteInput) =>
        transport.request<Site>({ method: 'PUT', path: `${root}/workspaces/${workspaceId}/sites/${siteId}`, body: input }),
      remove: (workspaceId: string, siteId: string) =>
        transport.request<void>({ method: 'DELETE', path: `${root}/workspaces/${workspaceId}/sites/${siteId}` }),
    },
    hostKeys: {
      list: (workspaceId: string) => transport.request<KnownHost[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/known-hosts` }),
      trust: (workspaceId: string, key: { host: string; port: number; keyType: string; fingerprint: string }) =>
        transport.request<KnownHost>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/known-hosts`, body: key }),
      forget: (workspaceId: string, key: { host: string; port: number; keyType: string }) =>
        transport.request<void>({ method: 'DELETE', path: `${root}/workspaces/${workspaceId}/known-hosts`, query: key }),
    },
    quick: {
      list: (workspaceId: string) =>
        transport.request<QuickConnection[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/quick-connections` }),
      create: (workspaceId: string, input: QuickConnectInput) =>
        transport.request<QuickConnection>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/quick-connections`, body: input }),
      close: (workspaceId: string, quickId: string) =>
        transport.request<void>({ method: 'DELETE', path: `${root}/workspaces/${workspaceId}/quick-connections/${quickId}` }),
      save: (workspaceId: string, quickId: string, input: { name: string; collectionId?: string }) =>
        transport.request<Site>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/quick-connections/${quickId}/save`, body: input }),
    },
    files: {
      list: (workspaceId: string, siteId: string, path: string) =>
        transport.request<RemoteEntry[]>({ method: 'GET', path: filesPath(workspaceId, siteId), query: { path } }),
      stat: (workspaceId: string, siteId: string, path: string) =>
        transport.request<RemoteEntry>({ method: 'GET', path: `${filesPath(workspaceId, siteId)}/entry`, query: { path } }),
      mkdir: (workspaceId: string, siteId: string, path: string) =>
        transport.request<RemoteEntry>({ method: 'POST', path: `${filesPath(workspaceId, siteId)}/directories`, body: { path } }),
      rename: (workspaceId: string, siteId: string, from: string, to: string) =>
        transport.request<RemoteEntry>({ method: 'POST', path: `${filesPath(workspaceId, siteId)}/rename`, body: { from, to } }),
      remove: (workspaceId: string, siteId: string, path: string, recursive = false) =>
        transport.request<void>({ method: 'DELETE', path: filesPath(workspaceId, siteId), query: { path, recursive: recursive || undefined } }),
      downloadUrl: (workspaceId: string, siteId: string, path: string, transferId?: string) =>
        transport.url(`${filesPath(workspaceId, siteId)}/content`, { path, transferId }),
      probeTimeZone: (workspaceId: string, siteId: string, path: string) =>
        transport.request<TimeZoneProbe>({ method: 'POST', path: `${filesPath(workspaceId, siteId)}/timezone-probe`, query: { path } }),
      checksum: (workspaceId: string, siteId: string, path: string, algorithm: ChecksumAlgorithm) =>
        transport.request<Checksum>({ method: 'GET', path: `${filesPath(workspaceId, siteId)}/checksum`, query: { path, algorithm } }),
      archiveUrl: (workspaceId: string, siteId: string, path: string, transferId?: string) =>
        transport.url(`${filesPath(workspaceId, siteId)}/archive`, { path, transferId }),
      download: (workspaceId: string, siteId: string, path: string, transferId?: string) =>
        transport.stream({ method: 'GET', path: `${filesPath(workspaceId, siteId)}/content`, query: { path, transferId } }),
      upload: (workspaceId: string, siteId: string, path: string, body: Blob | Uint8Array, options: UploadOptions = {}) =>
        transport.request<RemoteEntry>({
          method: 'PUT',
          path: `${filesPath(workspaceId, siteId)}/content`,
          query: { path, mode: options.mode, offset: options.offset, transferId: options.transferId, size: options.size },
          body,
          signal: options.signal,
        }),
    },
    imports: {
      filezilla: (workspaceId: string, file: Blob | Uint8Array, options: ImportOptions = {}) =>
        transport.request<ImportResult>({
          method: 'POST',
          path: `${root}/workspaces/${workspaceId}/imports/filezilla`,
          query: { dryRun: options.dryRun, passwords: options.passwords, onConflict: options.onConflict },
          body: file,
          contentType: 'application/xml',
        }),
    },
    activity: {
      list: (workspaceId: string, query: { siteId?: string; limit?: number } = {}) =>
        transport.request<ActivityEntry[]>({ method: 'GET', path: `${root}/workspaces/${workspaceId}/activity`, query }),
    },
    copies: {
      create: (workspaceId: string, input: CopyInput) =>
        transport.request<Transfer>({ method: 'POST', path: `${root}/workspaces/${workspaceId}/copies`, body: input }),
    },
    transfers: {
      list: (query: { workspaceId?: string; limit?: number } = {}) =>
        transport.request<Transfer[]>({ method: 'GET', path: `${root}/transfers`, query }),
      get: (transferId: string) => transport.request<Transfer>({ method: 'GET', path: `${root}/transfers/${transferId}` }),
      remove: (transferId: string) => transport.request<void>({ method: 'DELETE', path: `${root}/transfers/${transferId}` }),
      cancel: (transferId: string) => transport.request<void>({ method: 'POST', path: `${root}/transfers/${transferId}/cancel` }),
      pause: (transferId: string) => transport.request<Transfer>({ method: 'POST', path: `${root}/transfers/${transferId}/pause` }),
      resume: (transferId: string) => transport.request<Transfer>({ method: 'POST', path: `${root}/transfers/${transferId}/resume` }),
      setPriority: (transferId: string, priority: TransferPriority) =>
        transport.request<Transfer>({ method: 'PATCH', path: `${root}/transfers/${transferId}`, body: { priority } }),
    },
  }
}

/** The typed client returned by {@link createApi}. */
export type Api = ReturnType<typeof createApi>
