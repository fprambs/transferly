import { describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'

import { siteDefaults, siteSchema, toSiteInput } from './site-form'

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
 * - FTP and FTPS sites use their own default port, never a key, and keep the protocol of a stored site
 * - password sites send only the password and key sites only the key and its passphrase, and blank secrets are left out
 */
describe('site form', () => {
  it('starts empty for a new site', () => {
    expect(siteDefaults(undefined, 'c9')).toMatchObject({ name: '', collectionId: 'c9', port: '', authMethod: 'password', listTimezone: 'UTC' })
    expect(siteDefaults(undefined, undefined).collectionId).toBe('')
  })

  it('is prefilled from a stored site without its secrets', () => {
    expect(siteDefaults(stored, undefined)).toEqual({
      name: 'Prod',
      collectionId: 'c1',
      protocol: 'sftp',
      host: 'prod.example.com',
      port: '2222',
      username: 'deploy',
      authMethod: 'key',
      password: '',
      privateKey: '',
      passphrase: '',
      listTimezone: 'Europe/Madrid',
      charset: 'utf-8',
    })
    expect(siteDefaults({ ...stored, collectionId: undefined }, 'c2').collectionId).toBe('c2')
  })

  it('sends the character set of FTP sites and UTF-8 for SFTP ones', () => {
    const values = { ...siteDefaults(stored, undefined), charset: 'shift_jis' }

    expect(toSiteInput({ ...values, protocol: 'ftp' }).charset).toBe('shift_jis')
    expect(toSiteInput({ ...values, protocol: 'ftps' }).charset).toBe('shift_jis')
    expect(toSiteInput({ ...values, protocol: 'sftp' }).charset).toBe('utf-8')
    expect(siteDefaults(undefined, undefined).charset).toBe('utf-8')
  })

  it.each(['', '22', '65535'])('accepts port %j', (port) => {
    expect(siteSchema.safeParse({ ...siteDefaults(stored, undefined), port }).success).toBe(true)
  })

  it.each(['0', '65536', 'ssh'])('rejects port %j', (port) => {
    expect(siteSchema.safeParse({ ...siteDefaults(stored, undefined), port }).success).toBe(false)
  })

  it('builds FTP and FTPS sites: their own port, always a password', () => {
    const base = { ...siteDefaults(undefined, undefined), name: 'F', host: 'h', username: 'u', authMethod: 'key' as const, password: 'pw', privateKey: 'KEY', passphrase: 'ph' }

    for (const protocol of ['ftp', 'ftps'] as const) {
      expect(toSiteInput({ ...base, protocol })).toMatchObject({
        protocol,
        port: 21,
        authMethod: 'password',
        password: 'pw',
        privateKey: undefined,
        passphrase: undefined,
      })
    }
    expect(toSiteInput({ ...base, protocol: 'ftps', port: '990' }).port).toBe(990)
    expect(siteDefaults({ ...stored, protocol: 'ftps' }, undefined).protocol).toBe('ftps')
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
})
