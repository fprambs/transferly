import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { adminSession, mockSession, mockVault, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

import { vaultErrorKeyOf } from './errors'

/**
 * Covers {@link VaultChip} and {@link VaultDialog}.
 *
 * Scenarios:
 * - the chip shows whether the vault is not set up, locked or unlocked
 * - an unlocked vault is locked from the chip
 * - a locked vault is unlocked with the master password; a wrong one shows the error and clears the field
 * - the unlock form asks for a password
 * - setting the vault up validates length and confirmation, then shows the recovery kit once, which can be copied
 * - a rejected setup shows its message
 * - the dialog can be cancelled
 * - errors map to their message and unknown ones to the generic message
 */
describe('vault chip', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  it('locks an unlocked vault', async () => {
    let locked = 0
    server.use(http.post('/api/v1/vault/lock', () => (locked++, new HttpResponse(null, { status: 204 }))))
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'Vault unlocked' }))

    await waitFor(() => expect(locked).toBe(1))
  })

  it('unlocks a locked vault, and reports a wrong master password', async () => {
    mockVault({ initialized: true, unlocked: false })
    let attempts = 0
    server.use(
      http.post('/api/v1/vault/unlock', () => {
        attempts++
        return attempts === 1 ? problem(403, 'invalid_secret') : new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'Vault locked' }))
    const dialog = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    await user.click(within(dialog).getByRole('button', { name: 'Unlock' }))
    expect(await within(dialog).findByText('Enter the master password.')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('Master password'), 'wrong password!')
    await user.click(within(dialog).getByRole('button', { name: 'Unlock' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The master password is not right.')
    expect(within(dialog).getByLabelText('Master password')).toHaveValue('')

    mockVault({ initialized: true, unlocked: true })
    await user.type(within(dialog).getByLabelText('Master password'), 'right password!')
    await user.click(within(dialog).getByRole('button', { name: 'Unlock' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(await screen.findByRole('button', { name: 'Vault unlocked' })).toBeInTheDocument()
  })

  it('sets the vault up and shows the recovery kit once', async () => {
    mockVault({ initialized: false, unlocked: false })
    let sent: unknown
    server.use(
      http.post('/api/v1/vault/setup', async ({ request }) => {
        sent = await request.json()
        return HttpResponse.json({ recoveryKit: 'KIT-1234-ABCD' }, { status: 201 })
      }),
    )
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'Set up the vault' }))
    const dialog = await screen.findByRole('dialog', { name: 'Set up the vault' })

    await user.type(within(dialog).getByLabelText('Master password'), 'short')
    await user.click(within(dialog).getByRole('button', { name: 'Create vault' }))
    expect(await within(dialog).findByText('Use at least 12 characters.')).toBeInTheDocument()

    await user.clear(within(dialog).getByLabelText('Master password'))
    await user.type(within(dialog).getByLabelText('Master password'), 'a long enough secret')
    await user.type(within(dialog).getByLabelText('Repeat the master password'), 'something else here')
    await user.click(within(dialog).getByRole('button', { name: 'Create vault' }))
    expect(await within(dialog).findByText('The passwords do not match.')).toBeInTheDocument()

    await user.clear(within(dialog).getByLabelText('Repeat the master password'))
    await user.type(within(dialog).getByLabelText('Repeat the master password'), 'a long enough secret')
    await user.click(within(dialog).getByRole('button', { name: 'Create vault' }))

    expect(await screen.findByText('KIT-1234-ABCD')).toBeInTheDocument()
    expect(sent).toEqual({ masterPassword: 'a long enough secret' })

    await user.click(screen.getByRole('button', { name: 'Copy' }))
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'I have stored it' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows a rejected setup', async () => {
    mockVault({ initialized: false, unlocked: false })
    server.use(http.post('/api/v1/vault/setup', () => problem(409, 'vault_exists')))
    const user = userEvent.setup()
    await renderApp()
    await user.click(await screen.findByRole('button', { name: 'Set up the vault' }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Master password'), 'a long enough secret')
    await user.type(within(dialog).getByLabelText('Repeat the master password'), 'a long enough secret')
    await user.click(within(dialog).getByRole('button', { name: 'Create vault' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The vault already exists.')
  })

  it('can be cancelled', async () => {
    mockVault({ initialized: true, unlocked: false })
    const user = userEvent.setup()
    await renderApp()
    await user.click(await screen.findByRole('button', { name: 'Vault locked' }))

    await user.click(await screen.findByRole('button', { name: 'Cancel' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('maps failures to messages', () => {
    expect(vaultErrorKeyOf(new Error('boom'))).toBe('unknown')
  })
})
