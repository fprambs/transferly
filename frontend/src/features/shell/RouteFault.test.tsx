import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { renderWithI18n } from '@/test/render'

import { RouteFault } from './RouteFault'

/**
 * Covers {@link RouteFault}.
 *
 * Use cases:
 * - a failure no route handled shows a short message instead of the router's own screen, and its button reloads the page
 */
describe('RouteFault', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('explains the failure and reloads the page', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload })
    await renderWithI18n(<RouteFault />)

    expect(screen.getByRole('heading', { name: 'Something went wrong' })).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Reload' }))

    expect(reload).toHaveBeenCalledTimes(1)
  })
})
