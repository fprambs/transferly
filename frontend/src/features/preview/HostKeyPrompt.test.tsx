import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/lib/transport'
import { renderWithProviders } from '@/test/render'

import { PreviewBody } from './PreviewBody'
import type { PreviewSource } from './source'

/** A failure of a read about the key of the server. */
function keyFailure(code: 'host_key_unknown' | 'host_key_changed'): ApiError {
  const params = code === 'host_key_changed' ? { keyType: 'ssh-rsa', fingerprint: 'SHA256:new', trusted: ['SHA256:old'] } : { keyType: 'ssh-ed25519', fingerprint: 'SHA256:first' }
  return new ApiError({ type: 'about:blank', title: code, status: 409, code, params })
}

/** A text file whose reads fail with the given failures, in order, before they succeed. */
function failingSource(failures: Error[], trust?: PreviewSource['trust']): PreviewSource {
  return {
    key: `memory:${failures.length}:${String(trust !== undefined)}`,
    name: 'notes.txt',
    size: 5,
    modifiedAt: '2026-10-05T12:00:00Z',
    trust,
    read: async () => {
      const failure = failures.shift()
      if (failure !== undefined) {
        throw failure
      }
      return new TextEncoder().encode('hello')
    },
  }
}

/**
 * Covers {@link HostKeyPrompt} in the viewer: a read that met a server key on a new connection.
 *
 * Scenarios:
 * - a key other than the trusted ones shows them with the new one, is added next to them, and the file is read again
 * - a key not seen before is trusted with one click; when trusting fails it says why and can be tried again
 * - a source that cannot trust keys, and failures that are not about keys, show the usual failure
 */
describe('host key prompt', () => {
  it('adds a changed key and reads again', async () => {
    const user = userEvent.setup()
    const trust = vi.fn().mockResolvedValue(undefined)
    await renderWithProviders(<PreviewBody source={failingSource([keyFailure('host_key_changed')], trust)} />)
    expect(await screen.findByText('SHA256:old')).toBeInTheDocument()
    expect(screen.getByText('SHA256:new')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Trust this key too' }))
    expect(trust).toHaveBeenCalledWith({ keyType: 'ssh-rsa', fingerprint: 'SHA256:new', trusted: ['SHA256:old'] }, 'add')
    expect(await screen.findByText('hello')).toBeInTheDocument()
  })

  it('trusts a new key, and says when trusting fails', async () => {
    const user = userEvent.setup()
    const trust = vi.fn().mockRejectedValueOnce(new ApiError({ type: 'about:blank', title: 'forbidden', status: 403, code: 'forbidden' })).mockResolvedValue(undefined)
    await renderWithProviders(<PreviewBody source={failingSource([keyFailure('host_key_unknown')], trust)} />)
    const confirm = await screen.findByRole('button', { name: 'Trust and connect' })
    await user.click(confirm)
    expect(await screen.findByText('Something went wrong. Try again.')).toBeInTheDocument()
    await user.click(confirm)
    expect(trust).toHaveBeenLastCalledWith({ keyType: 'ssh-ed25519', fingerprint: 'SHA256:first' }, undefined)
    expect(await screen.findByText('hello')).toBeInTheDocument()
  })

  it('shows the usual failure for a source that cannot trust keys', async () => {
    await renderWithProviders(<PreviewBody source={failingSource([keyFailure('host_key_changed')])} />)
    expect(await screen.findByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('shows the usual failure when it is not about a key', async () => {
    await renderWithProviders(<PreviewBody source={failingSource([new Error('gone')], vi.fn())} />)
    expect(await screen.findByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })
})
