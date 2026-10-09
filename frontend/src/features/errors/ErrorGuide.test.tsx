import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'
import { renderWithI18n } from '@/test/render'

import { ErrorGuide } from './ErrorGuide'
import { guidedCodeOf } from './guide'

/** A failed request with a stable code. */
function failed(code: string): ApiError {
  return new ApiError({ type: 'about:blank', title: code, status: 422, code })
}

/**
 * Covers {@link ErrorGuide} and {@link guidedCodeOf} (4.16).
 *
 * Scenarios:
 * - a failed request or a code with guidance shows its probable cause, what to do, and the code to quote
 * - a code the interface derived for the jump host is explained as the site's own
 * - a failure without guidance, an unknown code or something that is not a failure shows nothing
 * - the guidance follows the language of the interface
 */
describe('ErrorGuide', () => {
  it('explains a failed request', async () => {
    await renderWithI18n(<ErrorGuide failure={failed('remote_auth_failed')} />)
    expect(screen.getByText('Probable cause')).toBeInTheDocument()
    expect(screen.getByText('The server refused the username, password or key.')).toBeInTheDocument()
    expect(screen.getByText('What to do')).toBeInTheDocument()
    expect(screen.getByText('Code: remote_auth_failed')).toBeInTheDocument()
  })

  it('explains the failed SSH category and server algorithms', async () => {
    const failure = new ApiError({ type: 'about:blank', title: 'SSH', status: 422, code: 'ssh_algorithm_incompatible', params: { category: 'mac', serverAlgorithms: 'hmac-sha1-96' } })
    await renderWithI18n(<ErrorGuide failure={failure} />)
    expect(screen.getByText('SSH negotiation failed (message authentication (MAC)). Server algorithms: hmac-sha1-96.')).toBeInTheDocument()
  })

  it('explains a connection timeout and manual retry', async () => {
    await renderWithI18n(<ErrorGuide failure={failed('remote_connection_timeout')} />, 'es')
    expect(screen.getByText(/El servidor no respondió dentro del plazo de conexión/)).toBeInTheDocument()
    expect(screen.getByText(/Los intentos automáticos se detuvieron/)).toBeInTheDocument()
  })

  it('explains missing and unknown SSH diagnostics without showing untrusted categories', async () => {
    await renderWithI18n(<>
      <ErrorGuide failure="ssh_algorithm_incompatible" />
      <ErrorGuide failure={new ApiError({ type: 'about:blank', title: 'SSH', status: 422, code: 'ssh_algorithm_incompatible', params: { category: 'untrusted-category', serverAlgorithms: 'ssh-rsa' } })} />
    </>)
    expect(screen.getByText('SSH negotiation failed (unspecified category). Server algorithms: not available.')).toBeInTheDocument()
    expect(screen.getByText('SSH negotiation failed (unspecified category). Server algorithms: ssh-rsa.')).toBeInTheDocument()
  })

  it('finds the guidance of codes', () => {
    expect(guidedCodeOf('vault_locked')).toBe('vault_locked')
    expect(guidedCodeOf('jump_wrong_passphrase')).toBe('wrong_passphrase')
    expect(guidedCodeOf(failed('host_key_changed'))).toBe('host_key_changed')
    expect(guidedCodeOf('unknown')).toBeUndefined()
    expect(guidedCodeOf('jump_unknown')).toBeUndefined()
    expect(guidedCodeOf(new Error('boom'))).toBeUndefined()
    expect(guidedCodeOf(null)).toBeUndefined()
  })

  it('shows nothing without guidance, and speaks Spanish', async () => {
    await renderWithI18n(
      <>
        <ErrorGuide failure="something_new" />
        <ErrorGuide failure="rate_limited" />
      </>,
      'es',
    )
    expect(screen.getAllByRole('definition')).toHaveLength(3)
    expect(screen.getByText('Espera un minuto e inténtalo de nuevo.')).toBeInTheDocument()
  })
})
