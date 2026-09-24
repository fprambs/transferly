import { act, render, screen } from '@testing-library/react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'

import { useUiStore } from '@/state/ui-store'

import { usePrefersDark, useThemeSync } from './use-theme'

/** Controllable stand-in for the browser color scheme media query. */
function stubColorScheme(initiallyDark: boolean) {
  let matches = initiallyDark
  const listeners = new Set<() => void>()
  const removed = vi.fn()
  vi.stubGlobal('matchMedia', () => ({
    get matches() {
      return matches
    },
    addEventListener: (_event: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_event: string, listener: () => void) => {
      removed()
      listeners.delete(listener)
    },
  }))
  return {
    removed,
    set(next: boolean) {
      matches = next
      listeners.forEach((listener) => listener())
    },
  }
}

/** Renders whether the operating system prefers a dark scheme. */
function PrefersDark() {
  return <p>{usePrefersDark() ? 'dark' : 'light'}</p>
}

/** Mounts the theme synchronization without rendering anything. */
function ThemeSync() {
  useThemeSync()
  return null
}

/**
 * Covers {@link usePrefersDark}.
 *
 * Scenarios:
 * - reads the current operating system preference
 * - follows later changes of the preference
 * - stops listening when unmounted
 * - reports a light scheme when rendered on the server
 */
describe('usePrefersDark', () => {
  it('reads the operating system preference', () => {
    stubColorScheme(true)
    render(<PrefersDark />)

    expect(screen.getByText('dark')).toBeInTheDocument()
  })

  it('follows changes of the preference', () => {
    const media = stubColorScheme(false)
    render(<PrefersDark />)
    expect(screen.getByText('light')).toBeInTheDocument()

    act(() => media.set(true))

    expect(screen.getByText('dark')).toBeInTheDocument()
  })

  it('stops listening when unmounted', () => {
    const media = stubColorScheme(false)
    const { unmount } = render(<PrefersDark />)

    unmount()

    expect(media.removed).toHaveBeenCalled()
  })

  it('reports a light scheme on the server', () => {
    stubColorScheme(true)

    expect(renderToString(<PrefersDark />)).toContain('light')
  })
})

/**
 * Covers {@link useThemeSync}.
 *
 * Scenarios:
 * - an explicit dark preference adds the dark class
 * - the system preference follows the operating system, including later changes
 * - an explicit light preference removes the dark class
 */
describe('useThemeSync', () => {
  it('applies an explicit dark preference', () => {
    stubColorScheme(false)
    useUiStore.setState({ theme: 'dark' })

    render(<ThemeSync />)

    expect(document.documentElement).toHaveClass('dark')
  })

  it('follows the operating system when the preference is system', () => {
    const media = stubColorScheme(false)
    useUiStore.setState({ theme: 'system' })
    render(<ThemeSync />)
    expect(document.documentElement).not.toHaveClass('dark')

    act(() => media.set(true))

    expect(document.documentElement).toHaveClass('dark')
  })

  it('removes the dark class for an explicit light preference', () => {
    stubColorScheme(true)
    document.documentElement.classList.add('dark')
    useUiStore.setState({ theme: 'light' })

    render(<ThemeSync />)

    expect(document.documentElement).not.toHaveClass('dark')
  })
})
