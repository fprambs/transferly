import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useTransport } from './context'
import { TransportProvider } from './TransportProvider'
import type { Transport } from './types'

/** Renders the identity of the transport found by {@link useTransport}. */
function Probe({ expected }: { expected: Transport }) {
  return <p>{useTransport() === expected ? 'same transport' : 'other transport'}</p>
}

/** Renders a component that requires a transport. */
function Orphan() {
  useTransport()
  return null
}

/**
 * Covers {@link TransportProvider} and {@link useTransport}.
 *
 * Scenarios:
 * - descendants receive the provided transport
 * - using the hook without a provider fails with a clear message
 */
describe('TransportProvider', () => {
  it('gives descendants the provided transport', () => {
    const transport: Transport = {
      health: () => Promise.resolve({ status: 'ok' }),
      ready: () => Promise.resolve({ status: 'ok' }),
      request: () => Promise.resolve(undefined as never),
      url: (path) => path,
      setCsrfToken: () => undefined,
    }
    render(
      <TransportProvider transport={transport}>
        <Probe expected={transport} />
      </TransportProvider>,
    )

    expect(screen.getByText('same transport')).toBeInTheDocument()
  })

  it('fails when the hook is used without a provider', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)

    expect(() => render(<Orphan />)).toThrow('useTransport must be used within a TransportProvider')
  })
})
