import { describe, expect, it } from 'vitest'

import { quickSchema, toQuickInput } from './schema'

/** A valid set of form values. */
const values = { host: ' example.com ', port: '', username: ' deploy ', password: 'secret' }

/**
 * Covers {@link quickSchema} and {@link toQuickInput}.
 *
 * Scenarios:
 * - an empty or in-range port is valid, anything else is not
 * - the request trims text, uses the protocol default port when empty and keeps the password as typed
 * - a typed port is sent as a number
 * - FTP uses TLS when the server offers it, as FileZilla's quick connect does, and SFTP takes no encryption
 */
describe('quick connect form', () => {
  it.each(['', '1', '22', '65535'])('accepts port %j', (port) => {
    expect(quickSchema.safeParse({ ...values, port }).success).toBe(true)
  })

  it.each(['0', '65536', 'ssh', '-1', '123456'])('rejects port %j', (port) => {
    expect(quickSchema.safeParse({ ...values, port }).success).toBe(false)
  })

  it('builds the request with the default port', () => {
    expect(toQuickInput('sftp', values)).toEqual({
      protocol: 'sftp',
      host: 'example.com',
      port: 22,
      username: 'deploy',
      authMethod: 'password',
      password: 'secret',
    })
  })

  it('builds the request with the typed port', () => {
    expect(toQuickInput('ftp', { ...values, port: '2121' }).port).toBe(2121)
  })

  it('asks FTP to use TLS when the server offers it, and leaves SFTP without an encryption', () => {
    expect(toQuickInput('ftp', values)).toMatchObject({ protocol: 'ftp', ftpEncryption: 'optional', port: 21 })
    expect(toQuickInput('sftp', values).ftpEncryption).toBeUndefined()
  })
})
