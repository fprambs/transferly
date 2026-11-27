import { createHash, randomBytes } from 'node:crypto'
import http from 'node:http'
import type { AddressInfo } from 'node:net'

import { AxeBuilder } from '@axe-core/playwright'
import { request as playwrightRequest, type APIRequestContext } from '@playwright/test'

import { expect, test } from '../fixtures.ts'
import { account } from '../global-setup.ts'

/** The WCAG rules the accessibility scans check, as in accessibility.spec.ts. */
const wcag = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']

/** Address of the application under test. */
const baseURL = process.env.TRANSFERLY_E2E_URL as string

/** What the terminal client keeps while the person approves it: its PKCE verifier and challenge, and its state. */
interface Pkce {
  /** The secret sent only with the exchange. */
  verifier: string
  /** The S256 challenge of the verifier, sent to the approval page. */
  challenge: string
  /** The value the callback must return unchanged. */
  state: string
}

/** Makes a fresh verifier, its S256 challenge and a state, as transferly-tui does. */
function pkce(): Pkce {
  const verifier = randomBytes(32).toString('base64url')
  return { verifier, challenge: createHash('sha256').update(verifier).digest('base64url'), state: randomBytes(16).toString('base64url') }
}

/** A stand-in for the loopback listener of transferly-tui: it answers the first callback and hands over its query. */
interface Callback {
  /** The address to put in redirect_uri. */
  redirectUri: string
  /** Resolves with the query of the first request to /callback. */
  received: Promise<URLSearchParams>
}

/** Listens on 127.0.0.1 at a free port, as transferly-tui does, until the test ends. */
async function listenForCallback(): Promise<Callback & { close: () => void }> {
  let deliver: (query: URLSearchParams) => void = () => undefined
  const received = new Promise<URLSearchParams>((resolve) => {
    deliver = resolve
  })
  const server = http.createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://127.0.0.1')
    if (url.pathname === '/callback') {
      deliver(url.searchParams)
    }
    response.writeHead(200, { 'Content-Type': 'text/plain' }).end('transferly-tui: you can close this tab')
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address() as AddressInfo
  return { redirectUri: `http://127.0.0.1:${port}/callback`, received, close: () => server.close() }
}

/** The address of the approval page for a request of the terminal client. */
function authorizePath(keys: Pkce, host: string, redirectUri?: string): string {
  const params = new URLSearchParams({ code_challenge: keys.challenge, code_challenge_method: 'S256', state: keys.state, client_name: 'transferly-tui', client_host: host })
  if (redirectUri !== undefined) {
    params.set('redirect_uri', redirectUri)
  }
  return `/cli/authorize?${params.toString()}`
}

/** An API client with no cookie, as the terminal client is. */
async function terminal(): Promise<APIRequestContext> {
  return playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
}

/** Exchanges a code as the terminal client does and returns the bearer token. */
async function exchange(client: APIRequestContext, code: string, keys: Pkce, redirectUri?: string): Promise<string> {
  const answer = await client.post('/api/v1/cli/token', { data: { code, codeVerifier: keys.verifier, ...(redirectUri === undefined ? {} : { redirectUri }) } })
  expect(answer.status()).toBe(200)
  return ((await answer.json()) as { token: string }).token
}

/** Reads the account a bearer token signs in as, or the status when it signs in as nobody. */
async function whoAmI(client: APIRequestContext, token: string): Promise<string | number> {
  const answer = await client.get('/api/v1/me', { headers: { Authorization: `Bearer ${token}` } })
  return answer.ok() ? ((await answer.json()) as { user: { email: string } }).user.email : answer.status()
}

/**
 * Signs the terminal client in through the browser end to end, against the running application (ADR-0029, T1.8).
 *
 * Scenarios:
 * - with a loopback listener: approving with the password sends the code and the state to 127.0.0.1, the code turns into a bearer token
 *   of the account, the session appears under Terminal sessions with its computer, and ending it there stops the token at once
 * - without a listener: the page shows the code, which turns into a token once and only once, and the token signs out on its own;
 *   the approval page and the code pass the WCAG scan
 * - denying sends access_denied with the state to the listener and signs nothing in
 * - a visitor without a session signs in first and lands back on the same request
 */
test.describe('terminal sign-in', () => {
  test('approves through the loopback callback and ends the session from Security', async ({ page }) => {
    const keys = pkce()
    const callback = await listenForCallback()
    try {
      await page.goto(authorizePath(keys, 'e2e-loopback', callback.redirectUri))
      await expect(page.getByRole('heading', { name: 'Sign in from the terminal?' })).toBeVisible()
      await expect(page.getByText(`transferly-tui on e2e-loopback asks to use your account ${account.email}.`)).toBeVisible()
      await page.getByLabel('Your password').fill(account.password)
      await page.getByRole('button', { name: 'Approve' }).click()

      const query = await callback.received
      expect(query.get('state')).toBe(keys.state)
      await expect(page.getByText('transferly-tui: you can close this tab')).toBeVisible()

      const client = await terminal()
      const token = await exchange(client, query.get('code') as string, keys, callback.redirectUri)
      expect(await whoAmI(client, token)).toBe(account.email)

      await page.goto('/?settings=security')
      const end = page.getByRole('button', { name: 'End the session of transferly-tui on e2e-loopback' })
      await end.click()
      await page.getByRole('dialog', { name: 'End the session of transferly-tui on e2e-loopback?' }).getByRole('button', { name: 'End session' }).click()
      await expect(end).toBeHidden()
      expect(await whoAmI(client, token)).toBe(401)
      await client.dispose()
    } finally {
      callback.close()
    }
  })

  test('shows the code to paste when the terminal cannot listen', async ({ page }) => {
    const keys = pkce()
    await page.goto(authorizePath(keys, 'e2e-paste'))
    await expect(page.getByRole('heading', { name: 'Sign in from the terminal?' })).toBeVisible()
    expect((await new AxeBuilder({ page }).withTags(wcag).analyze()).violations).toEqual([])
    await page.getByLabel('Your password').fill(account.password)
    await page.getByRole('button', { name: 'Approve' }).click()

    const code = (await page.getByLabel('Sign-in code').textContent())?.trim() ?? ''
    expect(code.length).toBeGreaterThan(40)
    await expect(page.getByText(/Never give this code to anyone/)).toBeVisible()
    expect((await new AxeBuilder({ page }).withTags(wcag).analyze()).violations).toEqual([])

    const client = await terminal()
    const token = await exchange(client, code, keys)
    expect(await whoAmI(client, token)).toBe(account.email)
    const again = await client.post('/api/v1/cli/token', { data: { code, codeVerifier: keys.verifier } })
    expect(((await again.json()) as { code: string }).code).toBe('invalid_grant')

    expect((await client.post('/api/v1/auth/logout', { headers: { Authorization: `Bearer ${token}` } })).status()).toBe(204)
    expect(await whoAmI(client, token)).toBe(401)
    await client.dispose()
  })

  test('sends the refusal back to the terminal', async ({ page }) => {
    const keys = pkce()
    const callback = await listenForCallback()
    try {
      await page.goto(authorizePath(keys, 'e2e-deny', callback.redirectUri))
      await page.getByRole('button', { name: 'Deny' }).click()

      const query = await callback.received
      expect(query.get('error')).toBe('access_denied')
      expect(query.get('state')).toBe(keys.state)
      expect(query.get('code')).toBeNull()
    } finally {
      callback.close()
    }
  })

  test.describe('without a session', () => {
    test.use({ storageState: { cookies: [], origins: [] } })

    test('signs in first and comes back to the request', async ({ page }) => {
      const keys = pkce()
      await page.goto(authorizePath(keys, 'e2e-visitor'))
      await expect(page).toHaveURL(/\/login\?redirect=/)
      await page.getByLabel('Email').fill(account.email)
      await page.getByLabel('Password').fill(account.password)
      await page.getByRole('button', { name: 'Sign in' }).click()

      await expect(page.getByRole('heading', { name: 'Sign in from the terminal?' })).toBeVisible()
      await expect(page).toHaveURL(new RegExp(`/cli/authorize\\?.*state=${keys.state}`))
    })
  })
})
