import { act, render, renderHook, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PanelImperativeHandle } from 'react-resizable-panels'

import { Group, layoutStorage, Panel, ResizeHandle, useCollapsible } from './resizable'

/** A panel handle whose collapsed state the test sets, recording what it is asked to do. */
function fakePanel(collapsed: boolean): PanelImperativeHandle & { calls: string[]; collapsed: boolean } {
  const panel = {
    calls: [] as string[],
    collapsed,
    collapse: () => {
      panel.calls.push('collapse')
      panel.collapsed = true
    },
    expand: () => {
      panel.calls.push('expand')
      panel.collapsed = false
    },
    getSize: () => ({ asPercentage: 0, inPixels: 0 }),
    isCollapsed: () => panel.collapsed,
    resize: () => undefined,
  }
  return panel
}

/**
 * Covers the resizable panel helpers (4.10).
 *
 * Scenarios:
 * - layouts are kept in local storage, and storage that refuses to be read or written does not break the page
 * - the separator is a named, focusable separator
 * - a collapsible panel follows its preference both ways: the preference collapses and expands it, and the user dragging it closed or open
 *   updates the preference; before the panel exists nothing is asked of it
 */
describe('resizable panels', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
  })

  it('keeps layouts in local storage, and survives storage that refuses', () => {
    layoutStorage.setItem('layout', '{"a":50}')
    expect(layoutStorage.getItem('layout')).toBe('{"a":50}')

    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(layoutStorage.getItem('layout')).toBeNull()
    expect(() => layoutStorage.setItem('layout', '{}')).not.toThrow()
  })

  it('draws a named separator between panels', () => {
    render(
      <Group id="test-group">
        <Panel id="a">a</Panel>
        <ResizeHandle aria-label="Resize the test panels" />
        <Panel id="b">b</Panel>
      </Group>,
    )
    expect(screen.getByRole('separator', { name: 'Resize the test panels' })).toBeInTheDocument()
  })

  it('keeps a collapsible panel and its preference in step', () => {
    const changes: boolean[] = []
    const { result, rerender } = renderHook(({ open }) => useCollapsible(open, (next) => changes.push(next)), { initialProps: { open: true } })
    act(() => result.current.onResize({ asPercentage: 0, inPixels: 0 }))
    expect(changes).toEqual([])

    const panel = fakePanel(true)
    act(() => result.current.panelRef(panel))
    expect(panel.calls).toEqual(['expand'])

    rerender({ open: false })
    expect(panel.calls).toEqual(['expand', 'collapse'])
    rerender({ open: false })
    expect(panel.calls).toEqual(['expand', 'collapse'])

    panel.collapsed = false
    act(() => result.current.onResize({ asPercentage: 30, inPixels: 300 }))
    expect(changes).toEqual([true])

    rerender({ open: true })
    panel.collapsed = true
    act(() => result.current.onResize({ asPercentage: 0, inPixels: 0 }))
    expect(changes).toEqual([true, false])
    act(() => result.current.onResize({ asPercentage: 0, inPixels: 0 }))
    rerender({ open: false })
    act(() => result.current.onResize({ asPercentage: 0, inPixels: 0 }))
    expect(changes).toEqual([true, false, false])
  })
})
