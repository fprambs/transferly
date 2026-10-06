import { mkdir, writeFile } from 'node:fs/promises'

import { request, type FullConfig } from '@playwright/test'

/** Account the browser tests sign in with; it is created on first use. */
const account = { email: 'e2e@example.com', password: 'a long e2e password' }

/** Master password of the test account's vault. */
const masterPassword = 'a long e2e master password'

/** Where the signed-in browser state is kept for every test. */
export const storageStatePath = 'e2e/.auth/state.json'

/** Where the identifier of the shared quick connection is kept. */
export const quickConnectionPath = 'e2e/.auth/quick.json'

/**
 * Creates the test account and its vault, or signs in and unlocks them when they exist, closes the quick connections earlier runs
 * left open, which count towards the limit per user, opens one quick connection to the
 * SFTP server of the test stack, trusting its current key also when a recreated server presents a new one, and saves the session for
 * every test.
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
  const { id: workspaceId } = ((await (await context.get('/api/v1/workspaces', { headers })).json()) as Array<{ id: string }>)[0]
  const quickConnections = `/api/v1/workspaces/${workspaceId}/quick-connections`
  const leftovers = (await (await context.get(quickConnections, { headers })).json()) as Array<{ id: string }>
  for (const leftover of leftovers) {
    await context.delete(`${quickConnections}/${leftover.id}`, { headers })
  }
  const quick = await context.post(quickConnections, {
    data: { connector: 'sftp', config: { host: 'sftp', port: '22', username: 'transferly', auth: 'password' }, secrets: { password: 'transferly' } },
    headers,
  })
  if (!quick.ok()) {
    throw new Error(`Could not open the quick connection to the test SFTP server: ${quick.status()}`)
  }
  const { id: quickId } = (await quick.json()) as { id: string }
  const files = `/api/v1/workspaces/${workspaceId}/sites/${quickId}/files`
  let listing = await context.get(files, { params: { path: '/' }, headers })
  if (listing.status() === 409) {
    const { params } = (await listing.json()) as { params: { keyType: string; fingerprint: string } }
    await context.post(`/api/v1/workspaces/${workspaceId}/known-hosts`, { data: { host: 'sftp', port: 22, ...params, mode: 'replace' }, headers })
    listing = await context.get(files, { params: { path: '/' }, headers })
  }
  if (!listing.ok()) {
    throw new Error(`The test SFTP server did not answer: ${listing.status()}`)
  }
  await mkdir('e2e/.auth', { recursive: true })
  await writeFile(quickConnectionPath, JSON.stringify({ quickId }))
  await context.storageState({ path: storageStatePath })
  await context.dispose()
}
