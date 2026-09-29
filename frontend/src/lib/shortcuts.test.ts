import { afterEach, describe, expect, it, vi } from 'vitest'

import { formatShortcut, holdsMod, isApple } from './shortcuts'

/**
 * Covers the shortcuts written for each system (4.14c).
 *
 * Scenarios:
 * - Windows and Linux read Ctrl and join the keys with +; Apple systems read ⌘ and the modifier symbols run together
 * - the system comes from the client hints when the browser gives them, otherwise from the platform
 * - either Ctrl or ⌘ holds the main modifier
 */
describe('shortcuts', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('writes shortcuts for each system', () => {
    expect(formatShortcut('Mod+Shift+K', false)).toBe('Ctrl+Shift+K')
    expect(formatShortcut('Alt+W', false)).toBe('Alt+W')
    expect(formatShortcut('Mod+Shift+K', true)).toBe('⌘⇧K')
    expect(formatShortcut('Alt+Ctrl+/', true)).toBe('⌥⌃/')
  })

  it('tells an Apple system apart', () => {
    vi.stubGlobal('navigator', { platform: 'MacIntel' })
    expect(isApple()).toBe(true)
    expect(formatShortcut('Mod+B')).toBe('⌘B')
    vi.stubGlobal('navigator', { platform: 'MacIntel', userAgentData: { platform: 'Windows' } })
    expect(isApple()).toBe(false)
    vi.stubGlobal('navigator', { platform: 'Linux x86_64' })
    expect(formatShortcut('Mod+B')).toBe('Ctrl+B')
  })

  it('accepts Ctrl or ⌘ as the main modifier', () => {
    expect(holdsMod({ ctrlKey: true, metaKey: false })).toBe(true)
    expect(holdsMod({ ctrlKey: false, metaKey: true })).toBe(true)
    expect(holdsMod({ ctrlKey: false, metaKey: false })).toBe(false)
  })
})
