import type { QueryClient } from '@tanstack/react-query'
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router'
import type { RouterHistory } from '@tanstack/react-router'

import { AppShell } from '@/features/shell/AppShell'
import { AuthScreen } from '@/features/auth/AuthScreen'
import type { Api } from '@/lib/api/use-api'
import type { Transport } from '@/lib/transport'

import { sessionQuery, setupQuery } from './session'

/** What every route can read: the query cache, the API client and the transport. */
export interface RouterContext {
  /** Cache of server state. */
  queryClient: QueryClient
  /** Typed client of the backend. */
  api: Api
  /** How calls reach the backend. */
  transport: Transport
}

/** Root route: renders whatever child matches. */
const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Outlet })

/** Home of a signed-in user; sends everybody else to setup or login. */
const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQuery(context.api, context.transport))
    if (session !== null) {
      return
    }
    const setupRequired = await context.queryClient.ensureQueryData(setupQuery(context.api))
    throw redirect({ to: setupRequired ? '/setup' : '/login' })
  },
  component: AppShell,
})

/** Sends a signed-in user home; anyone else may see the screen. */
async function redirectHomeWhenSignedIn(context: RouterContext): Promise<void> {
  const session = await context.queryClient.ensureQueryData(sessionQuery(context.api, context.transport))
  if (session !== null) {
    throw redirect({ to: '/' })
  }
}

/** Login screen. */
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  beforeLoad: ({ context }) => redirectHomeWhenSignedIn(context),
  component: () => <AuthScreen mode="login" />,
})

/** First-run setup screen; once the administrator exists it leads to login. */
const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/setup',
  beforeLoad: async ({ context }) => {
    await redirectHomeWhenSignedIn(context)
    if (!(await context.queryClient.ensureQueryData(setupQuery(context.api)))) {
      throw redirect({ to: '/login' })
    }
  },
  component: () => <AuthScreen mode="setup" />,
})

/** Route tree of the application. */
const routeTree = rootRoute.addChildren([homeRoute, loginRoute, setupRoute])

/** Builds the application router; tests pass a memory history. */
export function createAppRouter(context: RouterContext, history?: RouterHistory) {
  return createRouter({ routeTree, context, history })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
