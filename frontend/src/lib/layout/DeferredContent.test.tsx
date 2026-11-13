import { act, screen } from '@testing-library/react'
import { lazy, type ReactNode } from 'react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { renderWithI18n } from '@/test/render'
import { DeferredContent, DeferredOverlay } from './DeferredContent'

/** Checks pending, successful and failed feature imports without unmounting surrounding navigation. */
describe('deferred feature recovery', () => {
  it('keeps navigation while loading then displays the feature', async () => {
    let finish: () => void = () => {}
    const Feature = lazy(() => new Promise<{ default: () => ReactNode }>((resolve) => { finish = () => resolve({ default: () => <p>Loaded feature</p> }) }))
    await renderWithI18n(<><nav>Navigation</nav><DeferredContent><Feature /></DeferredContent></>)
    expect(screen.getByRole('status')).toHaveTextContent('Reading the file')
    expect(screen.getByText('Navigation')).toBeInTheDocument()
    act(() => finish())
    expect(await screen.findByText('Loaded feature')).toBeInTheDocument()
  })

  it('loads an overlay without a loading state, and offers the same recovery when it fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    let finish: () => void = () => {}
    const Overlay = lazy(() => new Promise<{ default: () => ReactNode }>((resolve) => { finish = () => resolve({ default: () => <p>Loaded overlay</p> }) }))
    const Broken = lazy(() => Promise.reject(new Error('module unavailable')))
    await renderWithI18n(<><nav>Navigation</nav><DeferredOverlay><Overlay /></DeferredOverlay><DeferredOverlay><Broken /></DeferredOverlay></>)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('This feature could not be loaded')
    act(() => finish())
    expect(await screen.findByText('Loaded overlay')).toBeInTheDocument()
    expect(screen.getByText('Navigation')).toBeInTheDocument()
  })

  it('contains a failed import and offers a page reload', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const Feature = lazy(() => Promise.reject(new Error('module unavailable')))
    await renderWithI18n(<><nav>Navigation</nav><DeferredContent><Feature /></DeferredContent></>)
    expect(await screen.findByRole('alert')).toHaveTextContent('This feature could not be loaded')
    expect(screen.getByText('Navigation')).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Reload page' }))
  })
})
