import { describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'

import { accessModesOf, siteDefaults, siteSchema, tabsOf, tabsWithErrors, toSiteInput } from './site-form'

/** A stored site. */
const stored: Site = {
  id: 's1',
  collectionId: 'c1',
  name: 'Prod',
  protocol: 'sftp',
  host: 'prod.example.com',
  port: 2222,
  username: 'deploy',
  authMethod: 'key',
  listTimezone: 'Europe/Madrid',
  charset: 'utf-8',
  speedLimitBps: 0,
  secrets: ['private_key'],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/**
 * Covers the site form helpers: {@link siteSchema}, {@link siteDefaults} and {@link toSiteInput}.
 *
 * Scenarios:
 * - a new site starts empty on the given collection, an existing one is prefilled without secrets
 * - the schema accepts empty or in-range ports and rejects anything else
 * - the request trims text, defaults port and time zone, and files the site in its collection
 * - the character set is kept for FTP and FTPS and is UTF-8 for SFTP
 * - the speed limit is shown in MiB/s and sent back in bytes per second, and a blank one means no cap
 * - FTP sites send their encryption, default to port 21 or 990 for implicit TLS and never use a key; new ones require TLS, stored ones keep
 *   their encryption, and a legacy FTPS answer reads as FTP
 * - password sites send only the password and key sites only the key and its passphrase, and blank secrets are left out
 * - every protocol may ask for the password when connecting, which sends and keeps none; SFTP offers a key and FTP and FTPS the anonymous
 *   user; an anonymous site sends the anonymous user and clears its password, and a stored one is read back as anonymous only when it has
 *   no password
 * - the character set tab is shown to FTP and FTPS only, and the tabs of the fields that are wrong are found
 * - a stored proxy and jump host are shown prefilled and sent back, in both authentication methods, with their own
 *   default ports when left blank, and neither is sent when not set
 * - a proxy needs a host and a valid port once a kind is chosen, and a jump host needs a valid port and a username
 *   once a host is given, whatever the site's own protocol; leaving both unset is always valid
 */
describe('site form', () => {
  it('starts empty for a new site', () => {
    expect(siteDefaults(undefined, 'c9')).toMatchObject({ name: '', collectionId: 'c9', port: '', accessMode: 'normal', listTimezone: 'UTC' })
    expect(siteDefaults(undefined, undefined).collectionId).toBe('')
  })

  it('is prefilled from a stored site without its secrets', () => {
    expect(siteDefaults(stored, undefined)).toEqual({
      name: 'Prod',
      collectionId: 'c1',
      protocol: 'sftp',
      ftpEncryption: 'explicit',
      host: 'prod.example.com',
      port: '2222',
      username: 'deploy',
      accessMode: 'key',
      password: '',
      privateKey: '',
      passphrase: '',
      listTimezone: 'Europe/Madrid',
      charset: 'utf-8',
      speedLimit: '',
      proxyKind: 'none',
      proxyHost: '',
      proxyPort: '',
      proxyUsername: '',
      proxyPassword: '',
      jumpHost: '',
      jumpPort: '',
      jumpUsername: '',
      jumpAuthMethod: 'password',
      jumpPassword: '',
      jumpPrivateKey: '',
      jumpPassphrase: '',
    })
    expect(siteDefaults({ ...stored, collectionId: undefined }, 'c2').collectionId).toBe('c2')
  })

  it('sends the character set of FTP sites and UTF-8 for SFTP ones', () => {
    const values = { ...siteDefaults(stored, undefined), charset: 'shift_jis' }

    expect(toSiteInput({ ...values, protocol: 'ftp' }).charset).toBe('shift_jis')
    expect(toSiteInput({ ...values, protocol: 'sftp' }).charset).toBe('utf-8')
    expect(siteDefaults(undefined, undefined).charset).toBe('utf-8')
  })

  it('shows the speed limit in MiB/s and sends it back in bytes per second', () => {
    const capped = { ...stored, speedLimitBps: 2 * 1024 * 1024 }
    expect(siteDefaults(capped, undefined).speedLimit).toBe('2')
    expect(toSiteInput({ ...siteDefaults(capped, undefined) }).speedLimitBps).toBe(2 * 1024 * 1024)
    expect(siteDefaults(stored, undefined).speedLimit).toBe('')
    expect(toSiteInput(siteDefaults(stored, undefined)).speedLimitBps).toBe(0)
  })

  it.each(['', '0.5', '1024'])('accepts speed limit %j', (speedLimit) => {
    expect(siteSchema.safeParse({ ...siteDefaults(stored, undefined), speedLimit }).success).toBe(true)
  })

  it.each(['0', '-1', 'fast'])('rejects speed limit %j', (speedLimit) => {
    expect(siteSchema.safeParse({ ...siteDefaults(stored, undefined), speedLimit }).success).toBe(false)
  })

  it.each(['', '22', '65535'])('accepts port %j', (port) => {
    expect(siteSchema.safeParse({ ...siteDefaults(stored, undefined), port }).success).toBe(true)
  })

  it.each(['0', '65536', 'ssh'])('rejects port %j', (port) => {
    expect(siteSchema.safeParse({ ...siteDefaults(stored, undefined), port }).success).toBe(false)
  })

  it('builds FTP sites with their encryption and their own port, always with a password', () => {
    const base = { ...siteDefaults(undefined, undefined), name: 'F', protocol: 'ftp' as const, host: 'h', username: 'u', accessMode: 'key' as const, password: 'pw', privateKey: 'KEY', passphrase: 'ph' }

    for (const ftpEncryption of ['optional', 'explicit', 'plain'] as const) {
      expect(toSiteInput({ ...base, ftpEncryption })).toMatchObject({
        protocol: 'ftp',
        ftpEncryption,
        port: 21,
        authMethod: 'password',
        password: 'pw',
        privateKey: undefined,
        passphrase: undefined,
      })
    }
    expect(toSiteInput({ ...base, ftpEncryption: 'implicit' }).port).toBe(990)
    expect(toSiteInput({ ...base, ftpEncryption: 'implicit', port: '2990' }).port).toBe(2990)
    expect(toSiteInput({ ...base, protocol: 'sftp' }).ftpEncryption).toBeUndefined()
  })

  it('reads the encryption of a stored FTP site, and requires TLS for new ones', () => {
    expect(siteDefaults(undefined, undefined).ftpEncryption).toBe('explicit')
    expect(siteDefaults({ ...stored, protocol: 'ftp', ftpEncryption: 'optional' }, undefined)).toMatchObject({ protocol: 'ftp', ftpEncryption: 'optional' })
    expect(siteDefaults({ ...stored, protocol: 'ftps' }, undefined).protocol).toBe('ftp')
  })

  it('builds a password site', () => {
    const values = { ...siteDefaults(undefined, 'c1'), name: ' Prod ', host: ' h ', username: ' u ', password: 'pw', privateKey: 'ignored', passphrase: 'ignored' }

    expect(toSiteInput(values)).toEqual({
      name: 'Prod',
      collectionId: 'c1',
      protocol: 'sftp',
      host: 'h',
      port: 22,
      username: 'u',
      authMethod: 'password',
      listTimezone: 'UTC',
      charset: 'utf-8',
      speedLimitBps: 0,
      password: 'pw',
      privateKey: undefined,
      passphrase: undefined,
    })
  })

  it('builds a key site with the typed port and time zone, leaving blank secrets out', () => {
    const values = { ...siteDefaults(stored, undefined), password: 'ignored', privateKey: 'KEY', listTimezone: '' }

    expect(toSiteInput(values)).toMatchObject({ port: 2222, authMethod: 'key', listTimezone: 'UTC', password: undefined, privateKey: 'KEY', passphrase: undefined })
    expect(toSiteInput({ ...values, privateKey: '' }).privateKey).toBeUndefined()
    expect(toSiteInput({ ...siteDefaults(undefined, undefined), collectionId: '' }).collectionId).toBeUndefined()
  })

  it('shows a stored proxy and jump host prefilled, in both authentication methods', () => {
    const proxied: Site = { ...stored, proxy: { kind: 'socks5', host: 'proxy.example.com', port: 1080, username: 'proxyuser' } }
    expect(siteDefaults(proxied, undefined)).toMatchObject({ proxyKind: 'socks5', proxyHost: 'proxy.example.com', proxyPort: '1080', proxyUsername: 'proxyuser' })

    const jumpedPassword: Site = { ...stored, jump: { host: 'bastion.example.com', port: 2222, username: 'bastion', authMethod: 'password' } }
    expect(siteDefaults(jumpedPassword, undefined)).toMatchObject({ jumpHost: 'bastion.example.com', jumpPort: '2222', jumpUsername: 'bastion', jumpAuthMethod: 'password' })

    const jumpedKey: Site = { ...stored, jump: { host: 'bastion.example.com', port: 22, username: 'bastion', authMethod: 'key' } }
    expect(siteDefaults(jumpedKey, undefined).jumpAuthMethod).toBe('key')

    expect(siteDefaults(stored, undefined)).toMatchObject({ proxyKind: 'none', proxyPort: '', jumpHost: '', jumpPort: '' })
  })

  it('sends a proxy with its own port when set, defaults the port when blank, and leaves out the username when blank', () => {
    const withProxy = { ...siteDefaults(stored, undefined), proxyKind: 'http' as const, proxyHost: ' proxy.example.com ', proxyPort: '3128', proxyUsername: ' proxyuser ', proxyPassword: 'secret' }
    expect(toSiteInput(withProxy)).toMatchObject({ proxy: { kind: 'http', host: 'proxy.example.com', port: 3128, username: 'proxyuser' }, proxyPassword: 'secret' })
    expect(toSiteInput({ ...withProxy, proxyPort: '' }).proxy).toMatchObject({ port: 1080 })
    expect(toSiteInput({ ...withProxy, proxyUsername: '' }).proxy?.username).toBeUndefined()
    expect(toSiteInput({ ...withProxy, proxyPassword: '' }).proxyPassword).toBeUndefined()
    expect(toSiteInput(siteDefaults(stored, undefined)).proxy).toBeUndefined()
    expect(toSiteInput(siteDefaults(stored, undefined)).proxyPassword).toBeUndefined()
  })

  it('sends a jump host with its own port and, for each authentication method, only the credential it needs', () => {
    const withJumpPassword = { ...siteDefaults(stored, undefined), jumpHost: 'bastion.example.com', jumpPort: '2200', jumpUsername: 'bastion', jumpAuthMethod: 'password' as const, jumpPassword: 'secret' }
    expect(toSiteInput(withJumpPassword)).toMatchObject({
      jump: { host: 'bastion.example.com', port: 2200, username: 'bastion', authMethod: 'password' },
      jumpPassword: 'secret', jumpPrivateKey: undefined, jumpPassphrase: undefined,
    })
    expect(toSiteInput({ ...withJumpPassword, jumpPort: '' }).jump).toMatchObject({ port: 22 })

    const withJumpKey = { ...withJumpPassword, jumpAuthMethod: 'key' as const, jumpPrivateKey: 'KEY', jumpPassphrase: 'phrase' }
    expect(toSiteInput(withJumpKey)).toMatchObject({ jump: { authMethod: 'key' }, jumpPassword: undefined, jumpPrivateKey: 'KEY', jumpPassphrase: 'phrase' })

    const noJump = toSiteInput(siteDefaults(stored, undefined))
    expect(noJump.jump).toBeUndefined()
    expect(noJump.jumpPassword).toBeUndefined()
    expect(noJump.jumpPrivateKey).toBeUndefined()
    expect(noJump.jumpPassphrase).toBeUndefined()
  })

  it('validates the proxy only once a kind is chosen, and the jump host only once a host is given', () => {
    const base = siteDefaults(stored, undefined)
    expect(siteSchema.safeParse(base).success).toBe(true)
    expect(siteSchema.safeParse({ ...base, proxyKind: 'socks5', proxyHost: '', proxyPort: '1080' }).success).toBe(false)
    expect(siteSchema.safeParse({ ...base, proxyKind: 'socks5', proxyHost: 'p.example', proxyPort: 'nope' }).success).toBe(false)
    expect(siteSchema.safeParse({ ...base, proxyKind: 'socks5', proxyHost: 'p.example', proxyPort: '1080' }).success).toBe(true)
    expect(siteSchema.safeParse({ ...base, jumpHost: 'b.example', jumpPort: 'nope', jumpUsername: 'u' }).success).toBe(false)
    expect(siteSchema.safeParse({ ...base, jumpHost: 'b.example', jumpPort: '22', jumpUsername: '' }).success).toBe(false)
    expect(siteSchema.safeParse({ ...base, jumpHost: 'b.example', jumpPort: '22', jumpUsername: 'u' }).success).toBe(true)
  })

  it('offers asking for the password and the anonymous user to every protocol, and a key to SFTP', () => {
    expect(accessModesOf('sftp')).toEqual(['normal', 'ask', 'key', 'anonymous'])
    expect(accessModesOf('ftp')).toEqual(['normal', 'ask', 'anonymous'])
  })

  it('builds a site that asks for its password: no password sent, clearing a stored one', () => {
    const values = { ...siteDefaults(undefined, undefined), name: 'Asks', host: 'h', username: 'u', accessMode: 'ask' as const, password: 'typed' }

    expect(toSiteInput(values)).toMatchObject({ username: 'u', authMethod: 'ask', password: '' })
    expect(siteDefaults({ ...stored, authMethod: 'ask', secrets: [] }, undefined).accessMode).toBe('ask')
  })

  it('builds an anonymous site of either protocol: the anonymous user and no secret sent', () => {
    const values = { ...siteDefaults(undefined, undefined), name: 'Public', protocol: 'ftp' as const, host: 'h', accessMode: 'anonymous' as const, username: '', password: 'typed' }

    expect(siteSchema.safeParse(values).success).toBe(true)
    expect(toSiteInput(values)).toMatchObject({ username: 'anonymous', authMethod: 'anonymous', password: undefined, privateKey: undefined })
    expect(siteSchema.safeParse({ ...values, protocol: 'sftp' }).success).toBe(true)
    expect(toSiteInput({ ...values, protocol: 'sftp' })).toMatchObject({ username: 'anonymous', authMethod: 'anonymous', password: undefined })
  })

  it('reads the access mode of a stored site from its logon type', () => {
    const anonymous: Site = { ...stored, protocol: 'sftp', authMethod: 'anonymous', username: 'anonymous', secrets: [] }

    expect(siteDefaults(anonymous, undefined).accessMode).toBe('anonymous')
    expect(siteDefaults({ ...anonymous, authMethod: 'password' }, undefined).accessMode).toBe('normal')
  })

  it('shows the character set tab to FTP and FTPS only, and finds the tabs of the fields that are wrong', () => {
    expect(tabsOf('sftp')).not.toContain('charset')
    expect(tabsOf('ftp')).toContain('charset')
    expect(tabsWithErrors({ speedLimit: { type: 'custom' }, username: { type: 'custom' } })).toEqual(new Set(['transfer', 'general']))
  })
})
