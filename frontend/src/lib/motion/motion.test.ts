import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { crossfade, glidePanels, panelGlideMs, prefersReducedMotion, refresh } from '.'

/**
 * Covers {@link glidePanels}, {@link prefersReducedMotion} and {@link refresh}.
 *
 * Scenarios:
 * - the panels may animate for a moment after a toggle, and a second toggle extends the moment
 * - reduced motion is read from the system, and is off where the browser cannot tell
 * - a refreshed element fades in again, unless the browser cannot animate, the person prefers reduced motion or there is no element
 * - a page change crossfades through a view transition when the browser has them and motion is welcome, and happens at once otherwise
 */
describe('motion', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('lets the panels glide for a moment after a toggle', () => {
    const root = document.createElement('html')
    glidePanels(root)
    expect(root.dataset.panelsMoving).toBe('')

    vi.advanceTimersByTime(panelGlideMs - 10)
    glidePanels(root)
    vi.advanceTimersByTime(panelGlideMs - 10)
    expect(root.dataset.panelsMoving).toBe('')

    vi.advanceTimersByTime(10)
    expect(root.dataset.panelsMoving).toBeUndefined()
  })

  it('glides the document by default', () => {
    glidePanels()
    expect(document.documentElement.dataset.panelsMoving).toBe('')
    vi.advanceTimersByTime(panelGlideMs)
    expect(document.documentElement.dataset.panelsMoving).toBeUndefined()
  })

  it('reads reduced motion from the system', () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }))
    expect(prefersReducedMotion()).toBe(true)
    vi.stubGlobal('matchMedia', undefined)
    expect(prefersReducedMotion()).toBe(false)
  })

  it('fades an element in again only when it can and may', () => {
    const element = document.createElement('div')
    const animate = vi.fn()
    Object.assign(element, { animate })

    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    refresh(element)
    expect(animate).toHaveBeenCalledTimes(1)

    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    refresh(element)
    expect(animate).toHaveBeenCalledTimes(1)

    refresh(document.createElement('div'))
    refresh(null)
    expect(animate).toHaveBeenCalledTimes(1)
  })

  it('crossfades a page change only when it can and may', () => {
    const update = vi.fn()
    const startViewTransition = vi.fn((change: () => void) => change())

    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    crossfade(update, { startViewTransition })
    expect(startViewTransition).toHaveBeenCalledTimes(1)

    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    crossfade(update, { startViewTransition })
    crossfade(update, {})
    crossfade(update)
    expect(startViewTransition).toHaveBeenCalledTimes(1)
    expect(update).toHaveBeenCalledTimes(4)
  })
})
