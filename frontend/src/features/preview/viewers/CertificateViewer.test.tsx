import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { CertificateViewer } from './CertificateViewer'

/** Reads a certificate sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/certificates', name)))
}

/** Shows a file of the viewer. */
async function show(name: string, bytes = sample(name)) {
  await renderWithProviders(<CertificateViewer source={bytesSource(name, bytes)} kind="certificate" />)
  return userEvent.setup()
}

afterEach(() => {
  vi.useRealTimers()
})

/**
 * Covers {@link CertificateViewer} (D2.7).
 *
 * Scenarios:
 * - a chain shows one card per certificate with its validity, authority and self-signed badges, its names, key, usages and fingerprints,
 *   and its source a click away
 * - an expired certificate says so, and a certificate not valid yet says so
 * - a certificate without a common name is named by its kind, and a certificate in DER shows without a source view
 * - a public key, a certificate request and OpenSSH keys, with and without a comment, show their facts
 * - a private key is never shown: a warning names it and its source is not offered; a damaged block says it cannot be read
 */
describe('CertificateViewer', () => {
  it('shows a chain of certificates', async () => {
    const user = await show('chain.pem')
    expect(await screen.findByText('2 items')).toBeInTheDocument()
    const [leaf, root] = screen.getAllByRole('heading', { level: 3 }).map((heading) => heading.closest('section') as HTMLElement)
    expect(within(leaf).getByText('files.example.com')).toBeInTheDocument()
    expect(within(leaf).getByText(/^Valid until /)).toBeInTheDocument()
    expect(within(leaf).getByText('files.example.com, *.files.example.com, 192.0.2.10')).toBeInTheDocument()
    expect(within(leaf).getByText('RSA · 2048 bits')).toBeInTheDocument()
    expect(within(leaf).getByText('serverAuth')).toBeInTheDocument()
    expect(within(leaf).getByText('SHA-256')).toBeInTheDocument()
    expect(within(leaf).queryByText('Self-signed')).not.toBeInTheDocument()
    expect(within(root).getByText('Certificate authority')).toBeInTheDocument()
    expect(within(root).getByText('Self-signed')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Source' }))
    expect(await screen.findByLabelText('Contents of chain.pem')).toHaveTextContent('BEGIN CERTIFICATE')
  })

  it('tells expired certificates and those not valid yet', async () => {
    await show('expired.pem')
    expect(await screen.findByText(/^Expired on /)).toBeInTheDocument()
    vi.useFakeTimers({ shouldAdvanceTime: true, now: new Date('2001-01-01T00:00:00Z') })
    await show('ec.crt')
    expect(await screen.findByText(/^Not valid until /)).toBeInTheDocument()
    expect(screen.getByText('EC · P-256')).toBeInTheDocument()
  })

  it('names a certificate without a common name by its kind', async () => {
    await show('dsa.pem')
    expect(await screen.findByRole('heading', { name: 'Certificate' })).toBeInTheDocument()
    expect(screen.getAllByText('O=DSA Samples')).toHaveLength(2)
  })

  it('shows a certificate in DER without a source view', async () => {
    await show('leaf.cer')
    expect(await screen.findByText('files.example.com')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Source' })).not.toBeInTheDocument()
  })

  it('shows public keys and requests', async () => {
    await show('public-key.pem')
    expect(await screen.findByText('Public key')).toBeInTheDocument()
    await show('leaf.csr')
    expect(await screen.findByText('Certificate request')).toBeInTheDocument()
    await show('id_ed25519.pub')
    expect(await screen.findByText('sample@transferly')).toBeInTheDocument()
    expect(screen.getByText(/^SHA256:/)).toBeInTheDocument()
    const line = new TextDecoder().decode(sample('id_ed25519.pub')).split(' ').slice(0, 2).join(' ')
    await show('bare.pub', new TextEncoder().encode(line))
    expect(await screen.findByText('SSH public key')).toBeInTheDocument()
  })

  it('never shows a private key', async () => {
    await show('private-key.pem')
    expect(await screen.findByText('Private key — contents not shown')).toBeInTheDocument()
    expect(screen.getByText('PRIVATE KEY')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Source' })).not.toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/MII[A-Za-z0-9+/]{20}/)
    await show('broken.pem')
    expect(await screen.findByText('This part cannot be read')).toBeInTheDocument()
  })
})
