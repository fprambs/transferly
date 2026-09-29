import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useNow } from './use-now'

/**
 * Covers {@link useNow}.
 *
 * Scenarios:
 * - it starts at the current instant and moves forward at the pace asked for, and stops when unmounted
 */
describe('useNow', () => {
  afterEach(() => vi.useRealTimers())

  it('moves forward at its pace', () => {
    vi.useFakeTimers({ now: new Date('2026-09-29T12:00:00Z') })
    const { result, unmount } = renderHook(() => useNow(30_000))
    expect(result.current.toISOString()).toBe('2026-09-29T12:00:00.000Z')

    act(() => vi.advanceTimersByTime(30_000))
    expect(result.current.toISOString()).toBe('2026-09-29T12:00:30.000Z')
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
