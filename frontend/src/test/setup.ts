import '@testing-library/jest-dom/vitest'

import { cleanup, configure } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, beforeEach, vi } from 'vitest'

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

configure({ asyncUtilTimeout: 4000 })

window.scrollTo = () => undefined

Object.defineProperties(HTMLElement.prototype, {
  offsetWidth: { configurable: true, get: () => 900 },
  offsetHeight: { configurable: true, get: () => 400 },
})

/** jsdom lacks this, and CodeMirror's measurement pass needs it to lay out the remote editor. */
const emptyRects: DOMRectList = Object.assign([] as DOMRect[], { item: () => null }) as DOMRectList
Range.prototype.getClientRects = () => emptyRects

beforeEach(() => {
  FakeSocket.reset()
  vi.stubGlobal('WebSocket', FakeSocket)
})

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))

afterEach(() => {
  cleanup()
  localStorage.clear()
  server.resetHandlers()
})

afterAll(() => server.close())
