import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { SecurityStatus } from '@/lib/api'
import { queryInnerDialog } from '@/test/dialogs'
import { adminSession, mockSession, mockSetupRequired, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

/** The state of the second factor the mock server holds. */
interface Factor {
  /** What the server reports. */
  status: SecurityStatus
  /** Every call the server saw, as `METHOD path body`. */
  calls: string[]
}

/** Answers the endpoints of the second factor of the signed-in account and records what was asked. */
function mockFactor(status: SecurityStatus = { totp: false, recoveryCodesLeft: 0, passkeys: [] }): Factor {
  const state: Factor = { status, calls: [] }
  const record = async (request: Request) => {
    const text = await request.text()
    state.calls.push(`${request.method} ${new URL(request.url).pathname} ${text}`.trim())
  }
  server.use(
    http.get('/api/v1/me/security', () => HttpResponse.json(state.status)),
    http.post('/api/v1/me/totp', async ({ request }) => {
      await record(request)
      return HttpResponse.json({ secret: 'JBSWY3DPEHPK3PXP', uri: 'otpauth://totp/Transferly:admin@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Transferly' })
    }),
    http.post('/api/v1/me/totp/confirm', async ({ request }) => {
      await record(request)
      state.status = { totp: true, recoveryCodesLeft: 10, passkeys: [] }
      return HttpResponse.json({ codes: ['AAAAA-BBBBB', 'CCCCC-DDDDD'] })
    }),
    http.post('/api/v1/me/totp/disable', async ({ request }) => {
      await record(request)
      state.status = { totp: false, recoveryCodesLeft: 0, passkeys: [] }
      return new HttpResponse(null, { status: 204 })
    }),
    http.post('/api/v1/me/recovery-codes', async ({ request }) => {
      await record(request)
      return HttpResponse.json({ codes: ['EEEEE-FFFFF'] })
    }),
  )
  return state
}

/** Answers the password step with a challenge. */
function mockChallenge(): void {
  server.use(http.post('/api/v1/auth/login', () => HttpResponse.json({ challenge: 'chal-1', methods: ['totp', 'recovery'] }, { status: 202 })))
}

/** Types the password step and submits it. */
async function password(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  await user.type(await screen.findByLabelText('Email'), 'admin@example.com')
  await user.type(screen.getByLabelText('Password'), 'a long enough password')
  await user.click(screen.getByRole('button', { name: 'Sign in' }))
}

/** Installs a fake browser that makes and uses passkeys, or refuses as it does when the user cancels, and returns how often each was asked. */
function installPasskeys(cancel = false): { created: number; used: number } {
  const calls = { created: 0, used: 0 }
  const answer = (kind: 'created' | 'used') => {
    calls[kind]++
    if (cancel) {
      throw new DOMException('cancelled', 'NotAllowedError')
    }
    return { toJSON: () => ({ id: kind }) }
  }
  vi.stubGlobal('PublicKeyCredential', {
    parseCreationOptionsFromJSON: (options: object) => options,
    parseRequestOptionsFromJSON: (options: object) => options,
  })
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    value: { create: async () => answer('created'), get: async () => answer('used') },
  })
  return calls
}

/** Answers the status with the passkeys given. */
function mockStatus(passkeys: SecurityStatus['passkeys']): void {
  server.use(http.get('/api/v1/me/security', () => HttpResponse.json({ totp: false, recoveryCodesLeft: passkeys.length > 0 ? 10 : 0, passkeys })))
}

/** Answers the password step with a challenge offering the methods given. */
function mockMethods(methods: string[]): void {
  server.use(http.post('/api/v1/auth/login', () => HttpResponse.json({ challenge: 'chal-1', methods }, { status: 202 })))
}

/** Types the password step and submits it. */
async function signIn(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  await user.type(await screen.findByLabelText('Email'), 'admin@example.com')
  await user.type(screen.getByLabelText('Password'), 'a long enough password')
  await user.click(screen.getByRole('button', { name: 'Sign in' }))
}

/**
 * Covers the second factor screens: {@link SecuritySection}, {@link EnableTotpDialog}, {@link PasswordDialog}, {@link RecoveryCodes}
 * and the second step of signing in in {@link MfaStep}.
 *
 * Scenarios:
 * - the settings show whether the second factor is on, with the recovery codes left
 * - turning it on shows the QR code and the key, refuses an empty or wrong code with a message, and a right one shows the recovery codes,
 *   which can be copied, and the section then reads on
 * - turning it off and renewing the recovery codes ask for the password, explain a wrong one, and show the new codes
 * - signing in with a second factor asks for a code, sends it with the challenge and opens the session; a wrong code is explained and
 *   clears the field, an expired challenge returns to the password, and going back returns to the password
 */
describe('second factor', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    Object.defineProperty(navigator, 'credentials', { configurable: true, value: undefined })
  })

  describe('settings', () => {
    beforeEach(() => {
      mockSession(adminSession)
      mockWorkspaceData()
    })

    it('shows it off and turns it on', async () => {
      const factor = mockFactor()
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      const section = (await screen.findByRole('heading', { name: 'Two-step verification' })).closest('section') as HTMLElement
      expect(await within(section).findByText('Off')).toBeInTheDocument()
      await user.click(within(section).getByRole('button', { name: 'Turn on' }))

      const dialog = await screen.findByRole('dialog', { name: 'Turn on two-step verification' })
      expect(await within(dialog).findByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument()
      expect(await within(dialog).findByRole('img', { name: /QR code/ })).toHaveAttribute('src', expect.stringMatching(/^data:image\/png/))
      await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('Enter the code.')

      server.use(http.post('/api/v1/me/totp/confirm', () => problem(422, 'invalid_code')))
      await user.type(within(dialog).getByLabelText('Code from the app'), '000000')
      await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('The code is not right.')

      mockFactor()
      await user.clear(within(dialog).getByLabelText('Code from the app'))
      await user.type(within(dialog).getByLabelText('Code from the app'), '123456')
      await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
      const codes = await screen.findByRole('dialog', { name: 'Save your recovery codes' })
      expect(within(codes).getByText('AAAAA-BBBBB')).toBeInTheDocument()
      await user.click(within(codes).getByRole('button', { name: 'Copy all' }))
      expect(await within(codes).findByRole('button', { name: 'Copied' })).toBeInTheDocument()
      expect(await navigator.clipboard.readText()).toBe('AAAAA-BBBBB\nCCCCC-DDDDD')
      expect(factor.calls[0]).toBe('POST /api/v1/me/totp')

      mockFactor({ totp: true, recoveryCodesLeft: 10, passkeys: [] })
      await user.click(within(codes).getByRole('button', { name: 'I have saved them' }))
      expect(await within(section).findByText('On')).toBeInTheDocument()
      expect(within(section).getByText('10 recovery codes left')).toBeInTheDocument()
    })

    it('can be cancelled while setting up', async () => {
      mockFactor()
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      await user.click(await screen.findByRole('button', { name: 'Turn on' }))
      const dialog = await screen.findByRole('dialog', { name: 'Turn on two-step verification' })
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

      await waitFor(() => expect(queryInnerDialog()).not.toBeInTheDocument())
    })

    it('turns it off with the password', async () => {
      const factor = mockFactor({ totp: true, recoveryCodesLeft: 4, passkeys: [] })
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      await user.click(await screen.findByRole('button', { name: 'Turn off' }))
      const dialog = await screen.findByRole('dialog', { name: 'Turn off two-step verification' })
      await user.click(within(dialog).getByRole('button', { name: 'Turn off' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('Enter your password.')

      server.use(http.post('/api/v1/me/totp/disable', () => problem(403, 'invalid_password')))
      await user.type(within(dialog).getByLabelText('Account password'), 'wrong password!')
      await user.click(within(dialog).getByRole('button', { name: 'Turn off' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('The password is not right.')
      expect(within(dialog).getByLabelText('Account password')).toHaveValue('')

      mockFactor({ totp: true, recoveryCodesLeft: 4, passkeys: [] })
      await user.type(within(dialog).getByLabelText('Account password'), 'a long enough password')
      await user.click(within(dialog).getByRole('button', { name: 'Turn off' }))
      await waitFor(() => expect(queryInnerDialog()).not.toBeInTheDocument())
      expect(factor.calls).toEqual([])
    })

    it('renews the recovery codes', async () => {
      const factor = mockFactor({ totp: true, recoveryCodesLeft: 4, passkeys: [] })
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      expect(await screen.findByText('4 recovery codes left')).toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'New recovery codes' }))
      const dialog = await screen.findByRole('dialog', { name: 'New recovery codes' })
      await user.type(within(dialog).getByLabelText('Account password'), 'a long enough password')
      await user.click(within(dialog).getByRole('button', { name: 'Create new codes' }))

      const codes = await screen.findByRole('dialog', { name: 'Save your recovery codes' })
      expect(within(codes).getByText('EEEEE-FFFFF')).toBeInTheDocument()
      expect(factor.calls).toEqual(['POST /api/v1/me/recovery-codes {"password":"a long enough password"}'])
    })
  })

  describe('passkeys in the settings', () => {
    beforeEach(() => {
      mockSession(adminSession)
      mockWorkspaceData()
    })

    it('lists the passkeys with their use and removes one with the password', async () => {
      mockStatus([
        { id: 'p1', name: 'Laptop', createdAt: '2026-09-01T12:00:00Z', lastUsedAt: '2026-09-20T12:00:00Z' },
        { id: 'p2', name: 'Key', createdAt: '2026-09-02T12:00:00Z' },
      ])
      let removed = ''
      server.use(
        http.post('/api/v1/me/passkeys/:id/remove', ({ params }) => {
          removed = String(params.id)
          return new HttpResponse(null, { status: 204 })
        }),
      )
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      const list = await screen.findByRole('list', { name: 'Passkeys' })
      expect(within(list).getByText(/Added Sep 1, 2026 · last used Sep 20, 2026/)).toBeInTheDocument()
      expect(within(list).getByText(/Added Sep 2, 2026 · never used/)).toBeInTheDocument()
      await user.click(within(list).getByRole('button', { name: 'Remove the passkey Laptop' }))
      const dialog = await screen.findByRole('dialog', { name: 'Remove the passkey Laptop' })
      await user.click(within(dialog).getByRole('button', { name: 'Remove' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('Enter your password.')

      server.use(http.post('/api/v1/me/passkeys/:id/remove', () => problem(403, 'invalid_password')))
      await user.type(within(dialog).getByLabelText('Account password'), 'wrong password!')
      await user.click(within(dialog).getByRole('button', { name: 'Remove' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('The password is not right.')

      server.use(
        http.post('/api/v1/me/passkeys/:id/remove', ({ params }) => {
          removed = String(params.id)
          return new HttpResponse(null, { status: 204 })
        }),
      )
      await user.type(within(dialog).getByLabelText('Account password'), 'a long enough password')
      await user.click(within(dialog).getByRole('button', { name: 'Remove' }))
      await waitFor(() => expect(queryInnerDialog()).not.toBeInTheDocument())
      expect(removed).toBe('p1')
    })

    it('says when there are none, adds one and can be cancelled', async () => {
      mockStatus([])
      const calls = installPasskeys()
      let finished: unknown
      server.use(
        http.post('/api/v1/me/passkeys/begin', () => HttpResponse.json({ ceremony: 'cer-1', options: { publicKey: { challenge: 'c' } } })),
        http.post('/api/v1/me/passkeys/finish', async ({ request }) => {
          finished = await request.json()
          return HttpResponse.json({ passkey: { id: 'p9', name: 'Laptop', createdAt: '2026-09-26T12:00:00Z' } }, { status: 201 })
        }),
      )
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      expect(await screen.findByText('No passkeys yet.')).toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Add a passkey' }))
      let dialog = await screen.findByRole('dialog', { name: 'Add a passkey' })
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
      await waitFor(() => expect(queryInnerDialog()).not.toBeInTheDocument())

      await user.click(screen.getByRole('button', { name: 'Add a passkey' }))
      dialog = await screen.findByRole('dialog', { name: 'Add a passkey' })
      await user.type(within(dialog).getByLabelText('Name'), ' Laptop ')
      await user.click(within(dialog).getByRole('button', { name: 'Continue' }))

      await waitFor(() => expect(queryInnerDialog()).not.toBeInTheDocument())
      expect(calls.created).toBe(1)
      expect(finished).toEqual({ ceremony: 'cer-1', name: 'Laptop', credential: { id: 'created' } })
    })

    it('shows the recovery codes the first passkey brings', async () => {
      mockStatus([])
      installPasskeys()
      server.use(
        http.post('/api/v1/me/passkeys/begin', () => HttpResponse.json({ ceremony: 'cer-1', options: {} })),
        http.post('/api/v1/me/passkeys/finish', () =>
          HttpResponse.json({ passkey: { id: 'p9', name: 'Laptop', createdAt: '2026-09-26T12:00:00Z' }, recoveryCodes: ['AAAAA-BBBBB'] }, { status: 201 }),
        ),
      )
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      await user.click(await screen.findByRole('button', { name: 'Add a passkey' }))
      const dialog = await screen.findByRole('dialog', { name: 'Add a passkey' })
      await user.type(within(dialog).getByLabelText('Name'), 'Laptop')
      await user.click(within(dialog).getByRole('button', { name: 'Continue' }))

      const codes = await screen.findByRole('dialog', { name: 'Save your recovery codes' })
      expect(within(codes).getByText('AAAAA-BBBBB')).toBeInTheDocument()
      await user.click(within(codes).getByRole('button', { name: 'I have saved them' }))
      await waitFor(() => expect(queryInnerDialog()).not.toBeInTheDocument())
    })

    it('explains a browser that cannot, a cancelled request and a refusal of the server', async () => {
      mockStatus([])
      const user = userEvent.setup()
      await renderApp({ path: '/?settings=security' })

      await user.click(await screen.findByRole('button', { name: 'Add a passkey' }))
      const dialog = await screen.findByRole('dialog', { name: 'Add a passkey' })
      await user.click(within(dialog).getByRole('button', { name: 'Continue' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('cannot use passkeys')

      installPasskeys(true)
      server.use(http.post('/api/v1/me/passkeys/begin', () => HttpResponse.json({ ceremony: 'cer-1', options: {} })))
      await user.click(within(dialog).getByRole('button', { name: 'Continue' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('The passkey was not used')

      server.use(http.post('/api/v1/me/passkeys/begin', () => problem(409, 'too_many_passkeys')))
      await user.click(within(dialog).getByRole('button', { name: 'Continue' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('most passkeys')
    })
  })

  describe('signing in', () => {
    beforeEach(() => {
      mockSession(null)
      mockSetupRequired(false)
      mockWorkspaceData()
    })

    it('asks for the code and opens the session', async () => {
      mockChallenge()
      let sent: unknown
      server.use(
        http.post('/api/v1/auth/mfa', async ({ request }) => {
          sent = await request.json()
          return HttpResponse.json(adminSession)
        }),
      )
      const user = userEvent.setup()
      const { history } = await renderApp({ path: '/login' })

      await password(user)
      expect(await screen.findByRole('heading', { name: 'Two-step verification' })).toBeInTheDocument()
      await user.click(await screen.findByRole('button', { name: 'Verify' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('Enter the code.')
      await user.type(screen.getByLabelText('Code'), ' 123 456 ')
      await user.click(screen.getByRole('button', { name: 'Verify' }))

      await waitFor(() => expect(history.location.pathname).toBe('/'))
      expect(sent).toEqual({ challenge: 'chal-1', code: '123 456' })
    })

    it('explains a wrong code and clears the field', async () => {
      mockChallenge()
      server.use(http.post('/api/v1/auth/mfa', () => problem(401, 'invalid_code')))
      const user = userEvent.setup()
      await renderApp({ path: '/login' })

      await password(user)
      await user.type(await screen.findByLabelText('Code'), '000000')
      await user.click(screen.getByRole('button', { name: 'Verify' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('The code is not right.')
      expect(screen.getByLabelText('Code')).toHaveValue('')
    })

    it('returns to the password when the challenge expired', async () => {
      mockChallenge()
      server.use(http.post('/api/v1/auth/mfa', () => problem(401, 'invalid_challenge')))
      const user = userEvent.setup()
      await renderApp({ path: '/login' })

      await password(user)
      await user.type(await screen.findByLabelText('Code'), '123456')
      await user.click(screen.getByRole('button', { name: 'Verify' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('The sign-in took too long.')
      expect(screen.getByLabelText('Password')).toBeInTheDocument()
    })

    it('goes back to the password step', async () => {
      mockChallenge()
      const user = userEvent.setup()
      await renderApp({ path: '/login' })

      await password(user)
      await user.click(await screen.findByRole('button', { name: 'Use another account' }))

      expect(await screen.findByLabelText('Password')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  describe('signing in with a passkey', () => {
    beforeEach(() => {
      mockSession(null)
      mockSetupRequired(false)
      mockWorkspaceData()
    })

    it('offers the passkey next to the code and signs in with it', async () => {
      mockMethods(['passkey', 'recovery'])
      const calls = installPasskeys()
      let sent: unknown
      server.use(
        http.post('/api/v1/auth/passkey/begin', () => HttpResponse.json({ ceremony: 'cer-1', options: { publicKey: {} } })),
        http.post('/api/v1/auth/passkey/finish', async ({ request }) => {
          sent = await request.json()
          return HttpResponse.json(adminSession)
        }),
      )
      const user = userEvent.setup()
      const { history } = await renderApp({ path: '/login' })

      await signIn(user)
      expect(await screen.findByLabelText('Code')).toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Use a passkey' }))

      await waitFor(() => expect(history.location.pathname).toBe('/'))
      expect(calls.used).toBe(1)
      expect(sent).toEqual({ challenge: 'chal-1', ceremony: 'cer-1', credential: { id: 'used' } })
    })

    it('shows only the passkey when there is no code to type', async () => {
      mockMethods(['passkey'])
      installPasskeys()
      const user = userEvent.setup()
      await renderApp({ path: '/login' })

      await signIn(user)

      expect(await screen.findByRole('button', { name: 'Use a passkey' })).toBeInTheDocument()
      expect(screen.queryByLabelText('Code')).not.toBeInTheDocument()
    })

    it('explains a cancelled request, a browser that cannot and a passkey the server refuses', async () => {
      mockMethods(['passkey', 'totp'])
      const user = userEvent.setup()
      await renderApp({ path: '/login' })

      await signIn(user)
      await user.click(await screen.findByRole('button', { name: 'Use a passkey' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('cannot use passkeys')

      installPasskeys(true)
      server.use(http.post('/api/v1/auth/passkey/begin', () => HttpResponse.json({ ceremony: 'cer-1', options: {} })))
      await user.click(screen.getByRole('button', { name: 'Use a passkey' }))
      expect(await screen.findByText(/The passkey was not used/)).toBeInTheDocument()

      installPasskeys()
      server.use(http.post('/api/v1/auth/passkey/finish', () => problem(401, 'invalid_passkey')))
      await user.click(screen.getByRole('button', { name: 'Use a passkey' }))
      expect(await screen.findByText(/The passkey was not accepted/)).toBeInTheDocument()
    })

    it('returns to the password when the challenge expired during the passkey', async () => {
      mockMethods(['passkey'])
      installPasskeys()
      server.use(http.post('/api/v1/auth/passkey/begin', () => problem(401, 'invalid_challenge')))
      const user = userEvent.setup()
      await renderApp({ path: '/login' })

      await signIn(user)
      await user.click(await screen.findByRole('button', { name: 'Use a passkey' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('The sign-in took too long.')
      expect(screen.getByLabelText('Password')).toBeInTheDocument()
    })
  })
})