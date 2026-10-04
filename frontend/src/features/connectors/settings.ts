/** A site or a quick connection: what has a connector and its configuration (ADR-0022). */
export interface Configured {
  /** The type of its connector, such as sftp or ftp. */
  connector: string
  /** The settings of its connector, by name. */
  config: Readonly<Record<string, string>>
}

/** A setting of a site or a quick connection, empty when it has none. */
export function settingOf(target: Configured, name: string): string {
  return target.config[name] ?? ''
}

/** The server a site or a quick connection reaches, from the host and port settings every network connector shares. */
export function hostOf(target: Configured): string {
  return settingOf(target, 'host')
}

/** The port of the server a site or a quick connection reaches; zero when it has none. */
export function portOf(target: Configured): number {
  return Number(settingOf(target, 'port') || 0)
}

/** The account a site or a quick connection signs in as. */
export function usernameOf(target: Configured): string {
  return settingOf(target, 'username')
}

/** The logon type of a site or a quick connection, such as password, ask or interactive. */
export function authOf(target: Configured): string {
  return settingOf(target, 'auth')
}

/** Where a site or a quick connection is, as the trusted server keys know it: its host and port. */
export function endpointOf(target: Configured): { host: string; port: number } {
  return { host: hostOf(target), port: portOf(target) }
}

/** What dialogs show of a site or a quick connection: its identifier, name, host and the account it signs in as. */
export function labelsOf(target: Configured & { id: string; name: string }): { id: string; name: string; host: string; username: string } {
  return { id: target.id, name: target.name, host: hostOf(target), username: usernameOf(target) }
}
