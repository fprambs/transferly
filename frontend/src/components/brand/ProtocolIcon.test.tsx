import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProtocolIcon, transportOf, type Transport } from './ProtocolIcon'

/**
 * Covers {@link transportOf} and {@link ProtocolIcon}.
 *
 * Scenarios:
 * - SFTP, FTPS and FTP with explicit or implicit TLS are encrypted; FTP that only uses TLS when offered is optional, plain FTP is not
 * - FTP without a stated encryption uses TLS when offered, as the server default
 * - each transport draws its own accent with its color, and the icon is decorative
 */
describe('ProtocolIcon', () => {
  it('tells the transport of a site', () => {
    expect(transportOf({ protocol: 'sftp' })).toBe('sftp')
    expect(transportOf({ protocol: 'ftps' })).toBe('ftps')
    expect(transportOf({ protocol: 'ftp', ftpEncryption: 'explicit' })).toBe('ftps')
    expect(transportOf({ protocol: 'ftp', ftpEncryption: 'implicit' })).toBe('ftps')
    expect(transportOf({ protocol: 'ftp', ftpEncryption: 'optional' })).toBe('optional')
    expect(transportOf({ protocol: 'ftp' })).toBe('optional')
    expect(transportOf({ protocol: 'ftp', ftpEncryption: 'plain' })).toBe('plain')
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
