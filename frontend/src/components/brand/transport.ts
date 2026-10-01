import type { Site } from '@/lib/api'

/** How a site reaches its server, as far as the user can tell from its icon. */
export type Transport = 'sftp' | 'ftps' | 'optional' | 'plain'

/** Tells the transport of a site: SFTP, FTP over TLS, FTP that uses TLS only when the server offers it, or FTP without encryption. */
export function transportOf(site: Pick<Site, 'protocol' | 'ftpEncryption'>): Transport {
  if (site.protocol === 'sftp') {
    return 'sftp'
  }
  if (site.protocol === 'ftps' || site.ftpEncryption === 'explicit' || site.ftpEncryption === 'implicit') {
    return 'ftps'
  }
  return site.ftpEncryption === 'plain' ? 'plain' : 'optional'
}
