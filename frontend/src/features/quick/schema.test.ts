import { describe, expect, it } from 'vitest'

import { builtinConnectors } from '@/test/handlers'

import { quickDefaults, quickResolver, toQuickInput, unattended, type QuickValues } from './schema'

/** The FTP and SFTP connectors with their real schemas. */
const [ftp, sftp] = builtinConnectors

/** Resolves values with the resolver of the quick connect form. */
function resolve(values: QuickValues) {
  return quickResolver(builtinConnectors)(values, undefined, { fields: {}, shouldUseNativeValidation: false })
}

/**
 * Covers the quick connect form helpers (ADR-0022).
 *
 * Scenarios:
 * - a new form starts empty on the given connector
 * - the request sends the settings given that apply and the secrets typed for fields that apply
 * - asking for the password, anonymous and interactive logons are left out, since a quick connection cannot use them
 * - the form checks the settings with the schema and asks for the secrets the logon needs, and for the account to be connected, as
 *   nothing is stored; an unknown connector is
 *   left to the server
 */
describe('quick connect form', () => {
  it('starts empty on a connector', () => {
    expect(quickDefaults('ftp')).toEqual({ connector: 'ftp', config: {}, secrets: {} })
  })

  it('builds the request from the settings and secrets that apply', () => {
    expect(toQuickInput({ connector: 'sftp', config: { host: ' example.com ', port: '', username: ' deploy ' }, secrets: { password: 'secret', private_key: 'K' } }, sftp)).toEqual({
      connector: 'sftp',
      config: { host: 'example.com', username: 'deploy' },
      secrets: { password: 'secret' },
    })
    expect(toQuickInput({ connector: 'ftp', config: { host: 'h', encryption: 'optional' }, secrets: {} }, ftp).config).toEqual({ host: 'h', encryption: 'optional' })
  })

  it('leaves out the logon types a quick connection cannot use', () => {
    expect(unattended.auth).toEqual(['ask', 'anonymous', 'interactive'])
  })

  it('checks the settings and asks for the secrets the logon needs', async () => {
    expect(await resolve({ connector: 'sftp', config: { port: '0' }, secrets: {} })).toEqual({
      values: {},
      errors: {
        config: { host: { type: 'schema', message: 'required' }, port: { type: 'schema', message: 'range' }, username: { type: 'schema', message: 'required' } },
        secrets: { password: { type: 'schema', message: 'required' } },
      },
    })
    const valid = { connector: 'sftp', config: { host: 'h', username: 'u' }, secrets: { password: 'pw' } }
    expect(await resolve(valid)).toEqual({ values: valid, errors: {} })
    expect(await resolve({ connector: 'gone', config: {}, secrets: {} })).toEqual({ values: { connector: 'gone', config: {}, secrets: {} }, errors: {} })
    expect((await resolve({ connector: 'gdrive', config: {}, secrets: {} })).errors).toMatchObject({ secrets: { refresh_token: { message: 'connect' } } })
  })
})
