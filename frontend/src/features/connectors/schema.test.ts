import { describe, expect, it } from 'vitest'

import type { Connector } from '@/lib/api'
import { builtinConnectors, connector } from '@/test/handlers'

import { applies, defaultOf, fixedOf, passphraseOf, hasGroup, holds, isSecret, missingReason, neededSecrets, settle, startingConnector, unconnected, toConfig, toSecrets, validate, validHost, type ConnectorField } from './schema'

/** The FTP and SFTP connectors with their real schemas. */
const [ftp, sftp] = builtinConnectors

/** Google Drive, whose account is signed in with Google. */
const drive = builtinConnectors.find((connector) => connector.type === 'gdrive') as Connector

/** A connector with one field of each kind the built-in ones lack. */
const sample = {
  ...connector('sample', 'Sample', true, 'beta'),
  fields: [
    { name: 'mode', kind: 'choice', group: 'connection', required: false, choices: ['a', 'b'] },
    { name: 'count', kind: 'number', group: 'connection', required: false },
    { name: 'bounded', kind: 'number', group: 'connection', required: false, min: 1, max: 10 },
    { name: 'capped', kind: 'number', group: 'connection', required: false, max: 5 },
    { name: 'secure', kind: 'boolean', group: 'connection', required: false },
    { name: 'note', kind: 'text', group: 'advanced', required: false, maxLength: 3 },
    { name: 'zone', kind: 'timezone', group: 'advanced', required: false },
  ] as ConnectorField[],
}

/**
 * Covers the schema helpers of the site form, which follow the rules of the server (ADR-0022).
 *
 * Scenarios:
 * - a condition holds for the values it lists, or for any value when it lists none; a field applies without a condition or when it holds
 * - secrets are told apart, a private key names its passphrase field, fixed values and dependent defaults are found for the values settled so far
 * - settling trims, fixes, fills in defaults and leaves out what does not apply, the secrets and the empty settings
 * - validation names each failing field with the reason the server gives, and passes a valid configuration
 * - hosts are addresses or dotted names
 * - the request sends the settings given that apply and are not fixed, and the secrets typed for fields that apply
 * - a new form starts on SFTP when it is offered, otherwise on the first connector, and on SFTP when none is known
 * - an account signed in with OAuth is a secret that must be connected, unless it is stored, and its absence reads as such
 * - a connection that keeps nothing needs the secrets its chosen values ask for, but not passphrases nor the secrets any value asks for
 */
describe('connector schema', () => {
  it('evaluates conditions and tells which fields apply', () => {
    expect(holds({ field: 'auth', in: ['key'] }, { auth: 'key' })).toBe(true)
    expect(holds({ field: 'auth', in: ['key'] }, {})).toBe(false)
    expect(holds({ field: 'jump_host' }, { jump_host: 'b' })).toBe(true)
    expect(holds({ field: 'jump_host', in: [] }, {})).toBe(false)
    const key = sftp.fields.find((field) => field.name === 'private_key') as ConnectorField
    expect(applies(key, { auth: 'key' })).toBe(true)
    expect(applies(key, { auth: 'password' })).toBe(false)
    expect(applies(sftp.fields[0], {})).toBe(true)
    expect(isSecret(key)).toBe(true)
    expect(isSecret(sftp.fields.find((field) => field.name === 'password') as ConnectorField)).toBe(true)
    expect(isSecret(sftp.fields[0])).toBe(false)
  })

  it('finds the passphrase field of a private key', () => {
    expect(passphraseOf(sftp.fields.find((field) => field.name === 'private_key') as ConnectorField)).toBe('passphrase')
    expect(passphraseOf(sftp.fields[0])).toBe('')
  })

  it('finds fixed values and dependent defaults', () => {
    const username = ftp.fields.find((field) => field.name === 'username') as ConnectorField
    const port = ftp.fields.find((field) => field.name === 'port') as ConnectorField
    expect(fixedOf(username, { auth: 'anonymous' })).toBe('anonymous')
    expect(fixedOf(username, { auth: 'password' })).toBeUndefined()
    expect(fixedOf(sftp.fields[0], {})).toBeUndefined()
    expect(defaultOf(port, { encryption: 'implicit' })).toBe('990')
    expect(defaultOf(port, { encryption: 'explicit' })).toBe('21')
    expect(defaultOf(sftp.fields[0], {})).toBe('')
  })

  it('settles values as the server does', () => {
    expect(settle(sftp, { host: ' h ', username: 'u', password: 'secret', jump_username: 'j' })).toEqual({ host: 'h', port: '22', username: 'u', auth: 'password', connections: '4', inexact_sizes: 'false', sudo: 'false', terminal: 'false' })
    expect(settle(ftp, { host: 'h', auth: 'anonymous', username: 'someone', encryption: 'implicit' })).toEqual({
      host: 'h',
      encryption: 'implicit',
      port: '990',
      auth: 'anonymous',
      username: 'anonymous',
      list_timezone: 'UTC',
      charset: 'utf-8',
      connections: '4',
    })
    expect(settle(sftp, { host: 'h', username: 'u', jump_host: 'b' })).toMatchObject({ jump_port: '22', jump_auth: 'password' })
  })

  it('names each failing field with the reason the server gives', () => {
    expect(validate(sftp, { host: 'h.example', username: 'u' })).toEqual({})
    expect(validate(sftp, {})).toEqual({ host: 'required', username: 'required' })
    expect(validate(sftp, { host: 'ftp://x', username: 'u', port: 'x' })).toEqual({ host: 'host', port: 'number' })
    expect(validate(sftp, { host: 'h', username: 'u', port: '70000' })).toEqual({ port: 'range' })
    expect(validate(sftp, { host: 'h', username: 'u', auth: 'account' })).toEqual({ auth: 'choice' })
    expect(validate(sftp, { host: 'h', username: 'x'.repeat(256) })).toEqual({ username: 'length' })
    expect(validate(sftp, { host: 'h', username: 'u', jump_host: 'b' })).toEqual({ jump_username: 'required' })
    expect(validate(sample, { count: '-5', bounded: '3', capped: '0', secure: 'true', note: 'abc', zone: 'America/Santiago' })).toEqual({})
    expect(validate(sample, { mode: 'c', bounded: '11', capped: '-1', secure: 'yes', note: 'abcd', zone: 'Mars/Base' })).toEqual({
      mode: 'choice',
      bounded: 'range',
      capped: 'range',
      secure: 'boolean',
      note: 'length',
      zone: 'timezone',
    })
    expect(validate(sample, { zone: 'UTC' })).toEqual({})
  })

  it('accepts addresses and dotted names as hosts', () => {
    for (const host of ['192.0.2.1', '2001:db8::1', 'files.example.com', 'my_host.local', 'example.com.']) {
      expect(validHost(host)).toBe(true)
    }
    for (const host of ['', 'sftp://h', 'h:22', 'h/x', 'a b', '-a.com', 'a.'.repeat(127) + 'com']) {
      expect(validHost(host)).toBe(false)
    }
  })

  it('sends the settings given that apply and are not fixed, and the secrets typed for fields that apply', () => {
    expect(toConfig(ftp, { host: ' h ', auth: 'anonymous', username: 'someone', port: '', jump_host: 'b' })).toEqual({ host: 'h', auth: 'anonymous' })
    expect(toSecrets(sftp, { auth: 'key' }, { private_key: 'KEY', passphrase: '', password: 'pw', token: 'x' })).toEqual({ private_key: 'KEY' })
    expect(toSecrets(sftp, {}, {})).toEqual({})
  })

  it('tells which accounts must be connected, and why a secret is missing', () => {
    expect(isSecret(drive.fields.find((field) => field.kind === 'oauth') as ConnectorField)).toBe(true)
    expect(neededSecrets(drive, {})).toEqual(['refresh_token'])
    expect(unconnected(drive, {}, {}, [])).toEqual(['refresh_token'])
    expect(unconnected(drive, {}, { refresh_token: 'r' }, [])).toEqual([])
    expect(unconnected(drive, {}, {}, ['refresh_token'])).toEqual([])
    expect(unconnected(sftp, {}, {}, [])).toEqual([])
    expect(missingReason(drive, 'refresh_token')).toBe('connect')
    expect(missingReason(sftp, 'password')).toBe('required')
  })

  it('tells which secrets a connection that keeps nothing needs', () => {
    expect(neededSecrets(sftp, {})).toEqual(['password'])
    expect(neededSecrets(sftp, { auth: 'key', proxy_kind: 'socks5', jump_host: 'b' })).toEqual(['private_key', 'jump_password'])
    expect(neededSecrets(ftp, { auth: 'account' })).toEqual(['password', 'account'])
    expect(neededSecrets(ftp, { auth: 'anonymous' })).toEqual([])
  })

  it('starts on SFTP when it is offered', () => {
    expect(startingConnector([ftp, sftp])).toBe('sftp')
    expect(startingConnector([ftp])).toBe('ftp')
    expect(startingConnector([])).toBe('sftp')
  })

  it('tells which groups a connector has', () => {
    expect(hasGroup(ftp, 'charset')).toBe(true)
    expect(hasGroup(sftp, 'charset')).toBe(false)
  })
})
