import { act, render } from '@testing-library/react'
import { StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useObjectUrl } from './object-url'

/** Shows the address of a blob. */
function Probe({ blob }: { blob: Blob | null }) {
  return <span>{useObjectUrl(blob) ?? 'none'}</span>
}

/**
 * Covers {@link useObjectUrl}.
 *
 * Scenarios:
 * - a blob gets an address that survives the double mount of StrictMode, and is revoked a moment after the component goes away
 * - no blob gives no address
 */
describe('useObjectUrl', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    let made = 0
    URL.createObjectURL = vi.fn(() => `blob:${++made}`)
    URL.revokeObjectURL = vi.fn()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps the address through a double mount and revokes it after the unmount', () => {
    const blob = new Blob(['x'])
    const view = render(
      <StrictMode>
        <Probe blob={blob} />
      </StrictMode>,
    )
    const url = view.container.textContent as string
    act(() => void vi.advanceTimersByTime(5000))
    expect(URL.revokeObjectURL).not.toHaveBeenCalled()
    view.unmount()
    expect(URL.revokeObjectURL).not.toHaveBeenCalled()
    act(() => void vi.advanceTimersByTime(1000))
    expect(URL.revokeObjectURL).toHaveBeenCalledWith(url)
  })

  it('gives no address without a blob', () => {
    const view = render(<Probe blob={null} />)
    expect(view.container.textContent).toBe('none')
  })
})
