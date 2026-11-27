import { describe, expect, it } from 'vitest'

import { callbackUrl, cliAuthorizeSearch, isLoopbackCallback, isWellFormed, leaveTo, type CliAuthorizeRequest } from './request'

/** A challenge of the right shape. */
const challenge = 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM'

/** A valid request with a loopback redirect, changed by the given fields. */
function request(changes: Partial<CliAuthorizeRequest> = {}): CliAuthorizeRequest {
  return { code_challenge: challenge, redirect_uri: 'http://127.0.0.1:49152/callback', client_name: 'transferly-tui', client_host: 'laptop', state: 's', ...changes }
}

/**
 * Covers how the approval page reads and answers a request of the terminal client.
 *
 * Scenarios:
 * - search values the router read as numbers or booleans become text, and malformed ones become empty
 * - spaces and line breaks copied with an address cut over several lines are dropped from the challenge, the state and the redirect
 * - only http loopback callbacks on 127.0.0.1 or [::1] with a port and nothing else count as the address of the terminal
 * - a request needs an S256 challenge of 43 base64url characters, a client name, and no redirect or a loopback callback
 * - the callback carries the answer and the state; without a state it carries the answer only
 * - leaving the application goes to the given address
 */
describe('cli request', () => {
  it('reads every search value as text', () => {
    expect(cliAuthorizeSearch.parse({ code_challenge: challenge, state: 123, client_name: 'tui', client_host: true })).toEqual({
      code_challenge: challenge,
      state: '123',
      client_name: 'tui',
      client_host: 'true',
    })
    expect(cliAuthorizeSearch.parse({ code_challenge: { x: 1 }, client_name: ['a'], redirect_uri: null })).toEqual({ code_challenge: '', client_name: '', redirect_uri: undefined })
    expect(
      cliAuthorizeSearch.parse({
        code_challenge: `${challenge.slice(0, 20)}\n  ${challenge.slice(20)}`,
        state: 'ab \r\ncd',
        redirect_uri: 'http://127.0.0.1:49152/ callback',
        client_name: 'transferly-tui',
        client_host: 'my laptop',
      }),
    ).toEqual({ code_challenge: challenge, state: 'abcd', redirect_uri: 'http://127.0.0.1:49152/callback', client_name: 'transferly-tui', client_host: 'my laptop' })
  })

  it.each(['http://127.0.0.1:49152/callback', 'http://[::1]:1/callback'])('accepts the callback %s', (address) => {
    expect(isLoopbackCallback(address)).toBe(true)
  })

  it.each([
    'http://localhost:8000/callback',
    'http://127.0.0.2:8000/callback',
    'https://127.0.0.1:8000/callback',
    'http://127.0.0.1/callback',
    'http://127.0.0.1:8000/other',
    'http://127.0.0.1:8000/callback?x=1',
    'http://127.0.0.1:8000/callback#x',
    'http://u:p@127.0.0.1:8000/callback',
    'https://evil.example.com/callback',
    'not a url',
  ])('refuses the callback %s', (address) => {
    expect(isLoopbackCallback(address)).toBe(false)
  })

  it('tells which requests can be shown', () => {
    expect(isWellFormed(request())).toBe(true)
    expect(isWellFormed(request({ redirect_uri: undefined, code_challenge_method: 'S256' }))).toBe(true)
    expect(isWellFormed(request({ redirect_uri: '' }))).toBe(true)
    expect(isWellFormed(request({ code_challenge: 'short' }))).toBe(false)
    expect(isWellFormed(request({ code_challenge: challenge.replace('E', '+') }))).toBe(false)
    expect(isWellFormed(request({ code_challenge_method: 'plain' }))).toBe(false)
    expect(isWellFormed(request({ client_name: '  ' }))).toBe(false)
    expect(isWellFormed(request({ redirect_uri: 'https://evil.example.com/' }))).toBe(false)
  })

  it('builds the callback with the answer and the state', () => {
    expect(callbackUrl('http://127.0.0.1:49152/callback', { code: 'c d' }, 's1')).toBe('http://127.0.0.1:49152/callback?code=c+d&state=s1')
    expect(callbackUrl('http://[::1]:5/callback', { error: 'access_denied' }, undefined)).toBe('http://[::1]:5/callback?error=access_denied')
  })

  it('leaves the application', () => {
    leaveTo('#left')
    expect(window.location.hash).toBe('#left')
  })
})
