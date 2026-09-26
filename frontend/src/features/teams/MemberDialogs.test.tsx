import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { teamWorkspace } from '@/test/teams'
import { renderWithProviders } from '@/test/render'

import { TransferDialog } from './MemberDialogs'

/**
 * Covers the corners of {@link TransferDialog} the members page cannot reach.
 *
 * Scenarios:
 * - with nobody to hand the workspace to, the dialog says so and the transfer button stays disabled
 */
describe('transfer dialog', () => {
  it('says when nobody can receive the workspace', async () => {
    await renderWithProviders(<TransferDialog workspace={teamWorkspace('owner')} candidates={[]} onClose={() => undefined} />)

    const dialog = await screen.findByRole('dialog', { name: 'Transfer ownership' })
    expect(within(dialog).getByText('No member can receive the workspace yet.')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Transfer' })).toBeDisabled()
  })
})
