import { expect, type APIRequestContext } from '@playwright/test'

import { totpCode } from './totp.ts'

/** Password of the accounts the tests create. */
const guestPassword = 'a long guest password'

/** What the signed-in account has to send with a change: the anti-CSRF token of its session. */
async function csrfOf(request: APIRequestContext): Promise<{ 'X-CSRF-Token': string }> {
  const me = (await (await request.get('/api/v1/me')).json()) as { csrfToken: string }
  return { 'X-CSRF-Token': me.csrfToken }
}

/** Creates an account the only way the instance allows, through an invitation to a team, and turns its authenticator app on. */
export async function createAccountWithSecondStep(admin: APIRequestContext, guest: APIRequestContext, email: string, team: string): Promise<void> {
  const headers = await csrfOf(admin)
  const workspace = (await (await admin.post('/api/v1/workspaces', { data: { name: team }, headers })).json()) as { id: string }
  const invitation = (await (await admin.post(`/api/v1/workspaces/${workspace.id}/invitations`, { data: { email, role: 'viewer' }, headers })).json()) as {
    token: string
  }
  const joined = await guest.post('/api/v1/invitations/join', { data: { token: invitation.token, password: guestPassword } })
  expect(joined.status()).toBe(201)
  const own = await csrfOf(guest)
  const setup = (await (await guest.post('/api/v1/me/totp', { headers: own })).json()) as { secret: string }
  const confirmed = await guest.post('/api/v1/me/totp/confirm', { data: { code: totpCode(setup.secret) }, headers: own })
  expect(confirmed.status()).toBe(200)
}
