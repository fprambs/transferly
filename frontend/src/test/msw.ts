import { setupServer } from 'msw/node'

/** Mock Service Worker server shared by the unit tests; see docs/guides/msw.md. */
export const server = setupServer()
