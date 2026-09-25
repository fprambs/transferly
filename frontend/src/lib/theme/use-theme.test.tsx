import { act, render, screen } from '@testing-library/react'
import { renderToString } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useUiStore } from '@/state/ui-store'

import { useActiveTheme, usePrefersDark, useThemeSync } from './use-theme'

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

/** Renders the name of the active theme. */
function ActiveTheme() {
  return <p>{useActiveTheme().name}</p>
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
 * Covers {@link useThemeSync} and {@link useActiveTheme}.
 *
 * Scenarios:
 * - a chosen dark theme is applied with its colors and the dark class
 * - a chosen light theme removes the dark class
 * - following the system wears the default theme by night and the light one by day, including later changes
 * - the active theme is the stored choice
 */
describe('useThemeSync', () => {
  beforeEach(() => {
    useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false })
    document.documentElement.classList.remove('dark')
  })

  it('applies a chosen dark theme', () => {
    stubColorScheme(false)
    useUiStore.setState({ themeId: 'monokai' })

    render(<ThemeSync />)

    expect(document.documentElement).toHaveClass('dark')
    expect(document.documentElement.dataset.theme).toBe('monokai')
    expect(document.documentElement.style.getPropertyValue('--primary')).toBe('#a6e22e')
  })

  it('removes the dark class for a chosen light theme', () => {
    stubColorScheme(true)
    document.documentElement.classList.add('dark')
    useUiStore.setState({ themeId: 'github-light' })

    render(<ThemeSync />)

    expect(document.documentElement).not.toHaveClass('dark')
    expect(document.documentElement.dataset.theme).toBe('github-light')
  })

  it('follows the operating system when asked to', () => {
    const media = stubColorScheme(false)
    useUiStore.setState({ themeId: 'monokai', followSystem: true })
    render(<ThemeSync />)
    expect(document.documentElement.dataset.theme).toBe('sentinel-light')

    act(() => media.set(true))

    expect(document.documentElement.dataset.theme).toBe('sentinel-dark')
    expect(document.documentElement).toHaveClass('dark')
  })

  it('reports the stored choice as the active theme', () => {
    stubColorScheme(false)
    useUiStore.setState({ themeId: 'nord' })

    render(<ActiveTheme />)

    expect(screen.getByText('Nord')).toBeInTheDocument()
  })
})
