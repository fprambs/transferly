import '@testing-library/jest-dom/vitest'

import { cleanup, configure } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, beforeEach, vi } from 'vitest'

import { useTabs } from '@/features/tabs/tabs-store'
import { usePrompts } from '@/features/prompts/prompts-store'
import { useDialogsStore } from '@/state/dialogs-store'
import { useConnectionState } from '@/features/toolbar/connection-store'

import { FakeSocket } from './fake-socket'
import { server } from './msw'

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }),
})

configure({ asyncUtilTimeout: process.env.VITEST_COVERAGE === '1' ? 10_000 : 4000 })

window.scrollTo = () => undefined

/** jsdom lacks it and the command palette scrolls its selected item into view with it; nothing scrolls in unit tests. */
Element.prototype.scrollIntoView ??= () => undefined

/** jsdom lacks it and the resizable panels observe their groups with it; nothing is measured in unit tests, so it observes nothing. */
class NoResizeObserver {
  /** Starts observing nothing. */
  observe(): void {}
  /** Stops observing nothing. */
  unobserve(): void {}
  /** Forgets every observation, of which there are none. */
  disconnect(): void {}
}
window.ResizeObserver ??= NoResizeObserver

/**
 * jsdom measures every element as an empty box at the origin, where the pointer of user-event also lands, so the resizable panels would take
 * every click for the start of a drag. Their groups, panels and separators are measured far off screen instead.
 */
const measure = Element.prototype.getBoundingClientRect
Element.prototype.getBoundingClientRect = function (this: Element): DOMRect {
  return this.matches('[data-group], [data-panel], [data-separator]') ? new DOMRect(-10_000, -10_000, 0, 0) : measure.call(this)
}

Object.defineProperties(HTMLElement.prototype, {
  offsetWidth: { configurable: true, get: () => 900 },
  offsetHeight: { configurable: true, get: () => 400 },
  clientHeight: { configurable: true, get: () => 400 },
})

/** jsdom lacks this, and CodeMirror's measurement pass needs it to lay out the remote editor. */
const emptyRects: DOMRectList = Object.assign([] as DOMRect[], { item: () => null }) as DOMRectList
Range.prototype.getClientRects = () => emptyRects

beforeEach(() => {
  useConnectionState.setState({ focused: null, disconnected: {} })
  useTabs.setState({ tabs: [], activeId: null })
  useDialogsStore.setState({ quickConnectOpen: false, paletteOpen: false })
  usePrompts.setState({ waiting: [] })
  FakeSocket.reset()
  vi.stubGlobal('WebSocket', FakeSocket)
})

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))

afterEach(() => {
  cleanup()
  localStorage.clear()
  server.resetHandlers()
})

afterAll(() => server.close())
