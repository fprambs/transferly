import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { narrowQuery, useNarrow } from './use-narrow'

/** The matchMedia of the test setup, put back after each test. */
const original = window.matchMedia

/**
 * Covers {@link useNarrow}.
 *
 * Scenarios:
 * - it reports whether the window matches the narrow width, and follows the window as it crosses it
 */
describe('useNarrow', () => {
  afterEach(() => {
    window.matchMedia = original
  })

  it('follows the window across the narrow width', () => {
    let matches = true
    const listeners: Array<() => void> = []
    window.matchMedia = ((query: string) => ({
      matches: query === narrowQuery && matches,
      media: query,
      addEventListener: (_: string, listener: () => void) => listeners.push(listener),
      removeEventListener: () => undefined,
    })) as unknown as typeof window.matchMedia

    const { result, unmount } = renderHook(() => useNarrow())
    expect(result.current).toBe(true)

    matches = false
    act(() => listeners.forEach((listener) => listener()))
    expect(result.current).toBe(false)
    unmount()
  })
})
