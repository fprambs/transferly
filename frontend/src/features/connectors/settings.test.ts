import { describe, expect, it } from 'vitest'

import { authOf, endpointOf, hostOf, labelsOf, portOf, settingOf, usernameOf } from './settings'

/** An SFTP site with every shared setting. */
const site = { id: 's1', name: 'Prod', connector: 'sftp', config: { host: 'prod.example.com', port: '2222', username: 'deploy', auth: 'key' } }

/** A configuration with none of the shared settings, as a connector without a network server has. */
const bare = { connector: 'bucket', config: {} }

/**
 * Covers the readers of the settings every network connector shares (ADR-0022).
 *
 * Scenarios:
 * - the host, port, username and logon type are read from the configuration, and the endpoint and the labels of dialogs are built from them
 * - a missing setting reads as empty, and a missing port as zero
 */
describe('connector settings', () => {
  it('reads the shared settings and builds the endpoint and labels from them', () => {
    expect(settingOf(site, 'host')).toBe('prod.example.com')
    expect(hostOf(site)).toBe('prod.example.com')
    expect(portOf(site)).toBe(2222)
    expect(usernameOf(site)).toBe('deploy')
    expect(authOf(site)).toBe('key')
    expect(endpointOf(site)).toEqual({ host: 'prod.example.com', port: 2222 })
    expect(labelsOf(site)).toEqual({ id: 's1', name: 'Prod', host: 'prod.example.com', username: 'deploy' })
  })

  it('reads a missing setting as empty and a missing port as zero', () => {
    expect(settingOf(bare, 'host')).toBe('')
    expect(endpointOf(bare)).toEqual({ host: '', port: 0 })
    expect(authOf(bare)).toBe('')
  })
})
