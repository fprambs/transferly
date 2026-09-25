import { request, type FullConfig } from '@playwright/test'

/** Account the browser tests sign in with; it is created on first use. */
const account = { email: 'e2e@example.com', password: 'a long e2e password' }

/** Master password of the test account's vault. */
const masterPassword = 'a long e2e master password'

/** Where the signed-in browser state is kept for every test. */
export const storageStatePath = 'e2e/.auth/state.json'

/**
 * Creates the test account and its vault, or signs in and unlocks them when they exist, and saves the session for every test.
 *
 * Doing it once per run keeps the tests clear of the rate limits of the account endpoints.
 */
export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0]?.use.baseURL
  const context = await request.newContext({ baseURL })
  let response = await context.post('/api/v1/setup', { data: account })
  if (response.status() === 409) {
    response = await context.post('/api/v1/auth/login', { data: account })
  }
  if (!response.ok()) {
    throw new Error(`Could not sign in the test account: ${response.status()}`)
  }
  const { csrfToken } = (await response.json()) as { csrfToken: string }
  const headers = { 'X-CSRF-Token': csrfToken }
  const vault = await context.post('/api/v1/vault/setup', { data: { masterPassword }, headers })
  if (vault.status() === 409) {
    const unlock = await context.post('/api/v1/vault/unlock', { data: { masterPassword }, headers })
    if (!unlock.ok()) {
      throw new Error(`Could not unlock the test vault: ${unlock.status()}`)
    }
  } else if (!vault.ok()) {
    throw new Error(`Could not set up the test vault: ${vault.status()}`)
  }
  await context.storageState({ path: storageStatePath })
  await context.dispose()
}
