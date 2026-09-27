import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { SecurityStatus } from '@/lib/api'
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
function mockFactor(status: SecurityStatus = { totp: false, recoveryCodesLeft: 0 }): Factor {
  const state: Factor = { status, calls: [] }
  const record = async (request: Request) => {
    const text = await request.text()
    state.calls.push(`${request.method} ${new URL(request.url).pathname} ${text}`.trim())
  }
  server.use(
    http.get('/api/v1/me/security', () => HttpResponse.json(state.status)),
    http.post('/api/v1/me/totp', async ({ request }) => {
      await record(request)
      return HttpResponse.json({ secret: 'JBSWY3DPEHPK3PXP', uri: 'otpauth://totp/Sentinel:admin@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Sentinel' })
    }),
    http.post('/api/v1/me/totp/confirm', async ({ request }) => {
      await record(request)
      state.status = { totp: true, recoveryCodesLeft: 10 }
      return HttpResponse.json({ codes: ['AAAAA-BBBBB', 'CCCCC-DDDDD'] })
    }),
    http.post('/api/v1/me/totp/disable', async ({ request }) => {
      await record(request)
      state.status = { totp: false, recoveryCodesLeft: 0 }
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
  describe('settings', () => {
    beforeEach(() => {
      mockSession(adminSession)
      mockWorkspaceData()
    })

    it('shows it off and turns it on', async () => {
      const factor = mockFactor()
      const user = userEvent.setup()
      await renderApp({ path: '/settings' })

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

      mockFactor({ totp: true, recoveryCodesLeft: 10 })
      await user.click(within(codes).getByRole('button', { name: 'I have saved them' }))
      expect(await within(section).findByText('On')).toBeInTheDocument()
      expect(within(section).getByText('10 recovery codes left')).toBeInTheDocument()
    })

    it('can be cancelled while setting up', async () => {
      mockFactor()
      const user = userEvent.setup()
      await renderApp({ path: '/settings' })

      await user.click(await screen.findByRole('button', { name: 'Turn on' }))
      const dialog = await screen.findByRole('dialog', { name: 'Turn on two-step verification' })
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    })

    it('turns it off with the password', async () => {
      const factor = mockFactor({ totp: true, recoveryCodesLeft: 4 })
      const user = userEvent.setup()
      await renderApp({ path: '/settings' })

      await user.click(await screen.findByRole('button', { name: 'Turn off' }))
      const dialog = await screen.findByRole('dialog', { name: 'Turn off two-step verification' })
      await user.click(within(dialog).getByRole('button', { name: 'Turn off' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('Enter your password.')

      server.use(http.post('/api/v1/me/totp/disable', () => problem(403, 'invalid_password')))
      await user.type(within(dialog).getByLabelText('Account password'), 'wrong password!')
      await user.click(within(dialog).getByRole('button', { name: 'Turn off' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('The password is not right.')
      expect(within(dialog).getByLabelText('Account password')).toHaveValue('')

      mockFactor({ totp: true, recoveryCodesLeft: 4 })
      await user.type(within(dialog).getByLabelText('Account password'), 'a long enough password')
      await user.click(within(dialog).getByRole('button', { name: 'Turn off' }))
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
      expect(factor.calls).toEqual([])
    })

    it('renews the recovery codes', async () => {
      const factor = mockFactor({ totp: true, recoveryCodesLeft: 4 })
      const user = userEvent.setup()
      await renderApp({ path: '/settings' })

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
})
