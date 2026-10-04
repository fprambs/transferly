import { describe, expect, it } from 'vitest'

import { transportOf } from './transport'

/**
 * Covers {@link transportOf}.
 *
 * Scenarios:
 * - SFTP and FTP with explicit or implicit TLS are encrypted; FTP that only uses TLS when offered is optional, plain FTP is not
 * - FTP without a stated encryption uses TLS when offered, as the server default
 * - S3 over HTTPS is encrypted, and S3 or Azure over plain HTTP is not
 */
describe('transportOf', () => {
  it('tells the transport of a site', () => {
    expect(transportOf({ connector: 'sftp', config: {} })).toBe('sftp')
    expect(transportOf({ connector: 'ftp', config: { encryption: 'explicit' } })).toBe('ftps')
    expect(transportOf({ connector: 'ftp', config: { encryption: 'implicit' } })).toBe('ftps')
    expect(transportOf({ connector: 'ftp', config: { encryption: 'optional' } })).toBe('optional')
    expect(transportOf({ connector: 'ftp', config: {} })).toBe('optional')
    expect(transportOf({ connector: 'ftp', config: { encryption: 'plain' } })).toBe('plain')
    expect(transportOf({ connector: 's3', config: { tls: 'true' } })).toBe('sftp')
    expect(transportOf({ connector: 's3', config: { tls: 'false' } })).toBe('plain')
    expect(transportOf({ connector: 'azfiles', config: { tls: 'false' } })).toBe('plain')
  })
})
