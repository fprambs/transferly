import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProtocolIcon } from './ProtocolIcon'
import { transportOf, type Transport } from './transport'

/**
 * Covers {@link transportOf} and {@link ProtocolIcon}.
 *
 * Scenarios:
 * - SFTP and FTP with explicit or implicit TLS are encrypted; FTP that only uses TLS when offered is optional, plain FTP is not
 * - FTP without a stated encryption uses TLS when offered, as the server default
 * - S3 over HTTPS is encrypted, and S3 or Azure over plain HTTP is not
 * - each transport draws its own accent with its color, and the icon is decorative
 */
describe('ProtocolIcon', () => {
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

  it.each<[Transport, string, string]>([
    ['sftp', 'text-primary', 'rect'],
    ['ftps', 'text-primary', 'path'],
    ['optional', 'text-warn', 'path'],
    ['plain', 'text-destructive', 'circle'],
  ])('draws %s with its accent', (transport, accent, shape) => {
    const { container } = render(<ProtocolIcon transport={transport} className="size-5" />)
    const svg = container.querySelector('svg') as SVGElement
    const group = svg.querySelector('g') as SVGGElement

    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveAttribute('data-transport', transport)
    expect(svg).toHaveClass('size-5')
    expect(group).toHaveClass(accent)
    expect(group.firstElementChild?.tagName).toBe(shape)
  })
})
