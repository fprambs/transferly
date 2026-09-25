import '@testing-library/jest-dom/vitest'

import { cleanup, configure } from '@testing-library/react'
import { afterAll, afterEach, beforeAll } from 'vitest'

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

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))

afterEach(() => {
  cleanup()
  localStorage.clear()
  server.resetHandlers()
})

afterAll(() => server.close())
