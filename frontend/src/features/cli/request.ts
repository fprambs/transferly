import { z } from 'zod'

/** Length of an S256 challenge: a SHA-256 in base64url without padding. */
const challengeLength = 43

/** The router reads search values as JSON when it can, so `state=123` arrives as a number; every field here is text. */
const text = z.preprocess((value) => (typeof value === 'number' || typeof value === 'boolean' ? String(value) : value), z.string())

/**
 * Search parameters of the approval page, named as in OAuth 2.0 (ADR-0029): the PKCE challenge and its method, the loopback address
 * to send the answer to, the state the terminal client checks, and what the client declares about itself. Malformed values become
 * empty, so the page can say the request is not valid instead of failing.
 */
export const cliAuthorizeSearch = z.object({
  code_challenge: text.catch(''),
  code_challenge_method: text.optional().catch(undefined),
  redirect_uri: text.optional().catch(undefined),
  state: text.optional().catch(undefined),
  client_name: text.catch(''),
  client_host: text.optional().catch(undefined),
})

/** A request of the terminal client as the approval page reads it. */
export type CliAuthorizeRequest = z.infer<typeof cliAuthorizeSearch>

/**
 * Tells whether an address is the loopback callback of the terminal client: `http://127.0.0.1:<port>/callback` or
 * `http://[::1]:<port>/callback`, with nothing else. Denying sends the browser there without asking the server, so the page checks it
 * itself and never becomes a way to send people to another site.
 */
export function isLoopbackCallback(address: string): boolean {
  let url: URL
  try {
    url = new URL(address)
  } catch {
    return false
  }
  return (
    url.protocol === 'http:' &&
    (url.hostname === '127.0.0.1' || url.hostname === '[::1]') &&
    url.port !== '' &&
    url.pathname === '/callback' &&
    url.search === '' &&
    url.hash === '' &&
    url.username === '' &&
    url.password === ''
  )
}

/**
 * Tells whether the request can be shown to approve: an S256 challenge of the right shape, a client name, and no redirect or a loopback
 * callback. The server checks everything again when approving.
 */
export function isWellFormed(request: CliAuthorizeRequest): boolean {
  return (
    /^[A-Za-z0-9_-]+$/.test(request.code_challenge) &&
    request.code_challenge.length === challengeLength &&
    (request.code_challenge_method ?? 'S256') === 'S256' &&
    request.client_name.trim() !== '' &&
    (request.redirect_uri === undefined || request.redirect_uri === '' || isLoopbackCallback(request.redirect_uri))
  )
}

/** The address that hands the answer back to the terminal client: its loopback callback with the answer and the state. */
export function callbackUrl(redirectUri: string, answer: { code: string } | { error: 'access_denied' }, state: string | undefined): string {
  const url = new URL(redirectUri)
  for (const [key, value] of Object.entries(answer)) {
    url.searchParams.set(key, value)
  }
  if (state !== undefined) {
    url.searchParams.set('state', state)
  }
  return url.toString()
}

/** Leaves the application for the callback of the terminal client. It is not a page of the application, so the router cannot go there. */
export function leaveTo(url: string): void {
  window.location.assign(url)
}
