import { describe, expect, it } from 'vitest'

import type { Site } from '@/lib/api'
import { builtinConnectors, connector } from '@/test/handlers'

import { connectorChoices, inputOf, siteDefaults, siteResolver, siteSchema, tabOfField, tabsOf, tabsWithErrors, toSiteInput, type SiteValues } from './site-form'

/** The SFTP and FTP connectors with their real schemas. */
const [ftp, sftp] = builtinConnectors

/** A stored SFTP site that signs in with a key. */
const stored: Site = {
  id: 's1',
  collectionId: 'c1',
  name: 'Prod',
  connector: 'sftp',
  config: { host: 'prod.example.com', port: '2222', username: 'deploy', auth: 'key' },
  remoteDir: '/srv/www',
  color: 'teal',
  speedLimitBps: 2 * 1024 * 1024,
  secrets: ['private_key'],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** The values of a form, with settings and secrets given. */
function values(config: Record<string, string>, secrets: Record<string, string> = {}, type = 'sftp'): SiteValues {
  return { name: ' Prod ', collectionId: '', color: '', connector: type, remoteDir: '', speedLimit: '', config, secrets }
}

/**
 * Covers the site form helpers (ADR-0022).
 *
 * Scenarios:
 * - the connectors offered are those the instance offers, plus the one an edited site already uses
 * - a new site starts empty on the given collection and connector, an existing one is prefilled with its settings but no secret
 * - the settings every site has are validated, and the speed limit is shown in MiB/s and sent back in bytes per second
 * - the request sends the settings that apply and were given, and only the secrets typed for fields that apply
 * - the form checks the settings every site has and those of the connector together; an unknown connector is left to the server
 * - an unchanged site is sent back as it is stored, without secrets
 * - the tabs follow the groups of the connector, and the tabs of the fields that are wrong are found
 */
describe('site form', () => {
  it('offers the connectors the instance offers and keeps the one of an edited site', () => {
    const s3 = connector('s3', 'S3', false)
    expect(connectorChoices([sftp, s3]).map((c) => c.type)).toEqual(['sftp'])
    expect(connectorChoices([sftp, s3], 's3').map((c) => c.type)).toEqual(['sftp', 's3'])
  })

  it('starts empty for a new site and prefills a stored one without its secrets', () => {
    expect(siteDefaults(undefined, 'c9', 'ftp')).toEqual({ name: '', collectionId: 'c9', color: '', connector: 'ftp', remoteDir: '', speedLimit: '', config: {}, secrets: {} })
    expect(siteDefaults(undefined, undefined, 'sftp').collectionId).toBe('')
    const prefilled = siteDefaults(stored, undefined, 'ftp')
    expect(prefilled).toMatchObject({ name: 'Prod', collectionId: 'c1', color: 'teal', connector: 'sftp', remoteDir: '/srv/www', speedLimit: '2', secrets: {} })
    expect(prefilled.config).toEqual(stored.config)
    expect(prefilled.config).not.toBe(stored.config)
    expect(siteDefaults({ ...stored, collectionId: undefined, color: undefined, remoteDir: undefined, speedLimitBps: 0 }, undefined, 'sftp')).toMatchObject({ collectionId: '', color: '', remoteDir: '', speedLimit: '' })
  })

  it('validates the settings every site has', () => {
    expect(siteSchema.safeParse(values({})).success).toBe(true)
    expect(siteSchema.safeParse({ ...values({}), name: ' ' }).error?.issues[0].message).toBe('nameRequired')
    expect(siteSchema.safeParse({ ...values({}), remoteDir: 'var' }).error?.issues[0].message).toBe('remoteDirInvalid')
    expect(siteSchema.safeParse({ ...values({}), speedLimit: '-1' }).error?.issues[0].message).toBe('speedLimitInvalid')
    expect(siteSchema.safeParse({ ...values({}), speedLimit: '1.5' }).success).toBe(true)
  })

  it('sends the settings that apply and the secrets typed for fields that apply', () => {
    const input = toSiteInput(
      { ...values({ host: ' h.example ', port: '', username: 'u', auth: 'key', jump_username: 'ignored' }, { private_key: 'KEY', passphrase: '', password: 'not used' }), collectionId: 'c1', speedLimit: '1.5', color: 'red', remoteDir: '/srv' },
      sftp,
    )
    expect(input).toEqual({
      name: 'Prod',
      collectionId: 'c1',
      connector: 'sftp',
      config: { host: 'h.example', username: 'u', auth: 'key' },
      secrets: { private_key: 'KEY' },
      remoteDir: '/srv',
      color: 'red',
      speedLimitBps: 1.5 * 1024 * 1024,
    })
    expect(toSiteInput(values({ host: 'h', username: 'u' }), sftp)).toMatchObject({ collectionId: undefined, speedLimitBps: 0, secrets: {} })
    expect(toSiteInput(values({ host: 'h', auth: 'anonymous', username: 'someone' }, {}, 'ftp'), ftp).config).toEqual({ host: 'h', auth: 'anonymous' })
  })

  it('checks the settings every site has and those of the connector together', async () => {
    const resolve = (given: SiteValues) => siteResolver(builtinConnectors)(given, undefined, { fields: {}, shouldUseNativeValidation: false })
    const invalid = await resolve({ ...values({ host: 'h', username: 'u', port: 'x' }), name: '' })
    expect(invalid.values).toEqual({})
    expect(invalid.errors).toMatchObject({ name: { message: 'nameRequired' }, config: { port: { type: 'schema', message: 'number' } } })
    expect((await resolve(values({ host: 'h', username: 'u' }))).errors).toEqual({})
    expect((await resolve(values({}, {}, 'gone'))).errors).toEqual({})
  })

  it('sends a stored site back as it is, without secrets', () => {
    expect(inputOf(stored)).toEqual({ name: 'Prod', collectionId: 'c1', connector: 'sftp', config: stored.config, remoteDir: '/srv/www', color: 'teal', speedLimitBps: stored.speedLimitBps })
    expect(inputOf({ ...stored, remoteDir: undefined, color: undefined })).toMatchObject({ remoteDir: '', color: '' })
  })

  it('follows the groups of the connector for its tabs, and finds the tabs of the fields that are wrong', () => {
    expect(tabsOf(sftp)).toEqual(['general', 'advanced', 'transfer', 'network'])
    expect(tabsOf(ftp)).toEqual(['general', 'advanced', 'transfer', 'charset', 'network'])
    expect(tabsOf(undefined)).toEqual(['general', 'advanced', 'transfer'])
    expect(tabOfField(ftp, 'charset')).toBe('charset')
    expect(tabOfField(ftp, 'nothing')).toBe('general')
    expect(tabOfField(undefined, 'host')).toBe('general')
    const wrong = tabsWithErrors({ name: { type: 'x' }, speedLimit: { type: 'x' }, unknown: { type: 'x' }, config: { proxy_host: { type: 'x' } }, secrets: { password: { type: 'x' } } } as never, sftp)
    expect([...wrong].sort()).toEqual(['general', 'network', 'transfer'])
    expect([...tabsWithErrors({ config: undefined } as never, sftp)]).toEqual([])
  })
})
