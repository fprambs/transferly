import { settingOf, type Configured } from '@/features/connectors/settings'

/** How a site reaches its server, as far as the user can tell from its icon. */
export type Transport = 'sftp' | 'ftps' | 'optional' | 'plain'

/** Tells the transport of a site: FTP over TLS, FTP that uses TLS only when the server offers it, or FTP without encryption, from its
 * encryption setting; every other connector is encrypted, as SFTP is. */
export function transportOf(site: Configured): Transport {
  if (site.connector !== 'ftp') {
    return 'sftp'
  }
  const encryption = settingOf(site, 'encryption')
  if (encryption === 'explicit' || encryption === 'implicit') {
    return 'ftps'
  }
  return encryption === 'plain' ? 'plain' : 'optional'
}
