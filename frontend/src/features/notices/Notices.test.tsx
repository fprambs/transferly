import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useUiStore } from '@/state/ui-store'
import { renderWithProviders } from '@/test/render'

import { Notices, noticeMs } from './Notices'
import { maxNotices, useNotices } from './notices-store'

/** The list of notices on show. */
function shown(): string[] {
  return within(screen.getByRole('region', { name: 'Notices' }))
    .queryAllByRole('listitem')
    .map((item) => item.textContent ?? '')
}

/**
 * Covers {@link Notices} and its store.
 *
 * Scenarios:
 * - a notice shows what was done and leaves by itself after a few seconds
 * - only the newest few show at once
 * - its button opens the connection log and takes it away, and the cross dismisses it
 */
describe('Notices', () => {
  beforeEach(() => {
    useNotices.setState({ notices: [], next: 1 })
    useUiStore.setState({ logOpen: false })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows a notice and takes it away after a while', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderWithProviders(<Notices />)

    act(() => useNotices.getState().notify('Renamed a.txt to b.txt.'))
    expect(shown()).toEqual([expect.stringContaining('Renamed a.txt to b.txt.')])
    act(() => vi.advanceTimersByTime(noticeMs + 10))
    expect(shown()).toEqual([])
  })

  it('keeps only the newest notices', async () => {
    await renderWithProviders(<Notices />)

    act(() => {
      for (let index = 0; index <= maxNotices; index++) {
        useNotices.getState().notify(`Notice ${index}`)
      }
    })

    expect(shown()).toHaveLength(maxNotices)
    expect(shown()[0]).toContain('Notice 1')
  })

  it('opens the log from a notice and dismisses another', async () => {
    const user = userEvent.setup()
    await renderWithProviders(<Notices />)
    act(() => {
      useNotices.getState().notify('Deleted a.txt.')
      useNotices.getState().notify('Deleted b.txt.')
    })

    await user.click(screen.getAllByRole('button', { name: 'View log' })[0])
    expect(useUiStore.getState().logOpen).toBe(true)
    expect(shown()).toEqual([expect.stringContaining('Deleted b.txt.')])

    await user.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(shown()).toEqual([])
  })
})
