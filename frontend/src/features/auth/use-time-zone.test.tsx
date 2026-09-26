import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { adminSession, mockSession } from '@/test/handlers'
import { renderWithProviders } from '@/test/render'

import { useTimeZone } from './use-time-zone'

/** Shows the time zone in use. */
function Zone() {
  return <p>zone {useTimeZone()}</p>
}

/**
 * Covers {@link useTimeZone}.
 *
 * Scenarios:
 * - the browser's time zone is used until the session is known, and the account's afterwards
 */
describe('useTimeZone', () => {
  it('uses the browser zone first and the account zone once it is known', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, timezone: 'Asia/Tokyo' } })

    await renderWithProviders(<Zone />)

    expect(screen.getByText(new RegExp(`^zone (?!Asia/Tokyo)`))).toBeInTheDocument()
    expect(await screen.findByText('zone Asia/Tokyo')).toBeInTheDocument()
  })
})
