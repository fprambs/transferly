import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ApiError } from '@/lib/transport'
import { mockUnlocking } from '@/test/handlers'
import { renderWithProviders } from '@/test/render'

import { PreviewBody } from './PreviewBody'
import type { PreviewSource } from './source'

/** A text file whose first read fails because the vault of the session is locked. */
function lockedSource(): PreviewSource {
  let reads = 0
  return {
    key: 'memory:locked',
    name: 'notes.txt',
    size: 5,
    modifiedAt: '2026-10-05T12:00:00Z',
    read: async () => {
      reads++
      if (reads === 1) {
        throw new ApiError({ type: 'about:blank', title: 'vault_locked', status: 423, code: 'vault_locked' })
      }
      return new TextEncoder().encode('hello')
    },
  }
}

/**
 * Covers {@link ReadFailure} in the viewer.
 *
 * Scenarios:
 * - a read that fails because the vault is locked opens the master password dialog at once, and reads again once it is unlocked,
 *   instead of offering a retry that would fail the same way
 */
describe('read failure', () => {
  it('unlocks the vault and reads again', async () => {
    const vault = mockUnlocking()
    const user = userEvent.setup()
    await renderWithProviders(<PreviewBody source={lockedSource()} />)
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Master password'), 'right password!')
    await user.click(within(dialog).getByRole('button', { name: /unlock/i }))
    expect(await screen.findByText('hello')).toBeInTheDocument()
    expect(vault.unlocked()).toBe(true)
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()
  })
})
