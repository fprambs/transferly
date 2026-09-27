import type { QueryClient } from '@tanstack/react-query'
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router'
import type { RouterHistory } from '@tanstack/react-router'
import { z } from 'zod'

import { TrustedServers } from '@/features/servers/TrustedServers'
import { AdminPage } from '@/features/admin/AdminPage'
import { AuditPage } from '@/features/teams/AuditPage'
import { AcceptInvitationPage } from '@/features/teams/AcceptInvitationPage'
import { MembersPage } from '@/features/teams/MembersPage'
import { SettingsPage } from '@/features/settings/SettingsPage'
import { NotFound } from '@/features/shell/NotFound'
import { EmptyExplorer } from '@/features/explorer/EmptyExplorer'
import { SiteExplorer } from '@/features/explorer/SiteExplorer'
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
const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Outlet, notFoundComponent: NotFound })

/** Layout of everything a signed-in user sees; it sends everybody else to setup or login. */
const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'shell',
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

/** Home of a signed-in user: no site is open yet. */
const homeRoute = createRoute({ getParentRoute: () => shellRoute, path: '/', component: EmptyExplorer })

/** Settings of the interface. */
const settingsRoute = createRoute({ getParentRoute: () => shellRoute, path: '/settings', component: SettingsPage })

/** Members and invitations of the team workspace in use. */
const membersRoute = createRoute({ getParentRoute: () => shellRoute, path: '/members', component: MembersPage })

/** Audit trail of the team workspace in use. */
const auditRoute = createRoute({ getParentRoute: () => shellRoute, path: '/audit', component: AuditPage })

/** Administration of the instance. */
const adminRoute = createRoute({ getParentRoute: () => shellRoute, path: '/admin', component: AdminPage })

/** Servers whose key the user trusted. */
const serversRoute = createRoute({ getParentRoute: () => shellRoute, path: '/servers', component: TrustedServers })

/**
 * A saved site or quick connection, browsed at the folder given by the `path` search parameter. The `split` parameter opens a
 * second pane on another connection, at the folder of `splitPath`.
 */
const siteRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: '/sites/$siteId',
  validateSearch: z.object({ path: z.string().catch('/'), split: z.string().optional().catch(undefined), splitPath: z.string().optional().catch(undefined) }),
  component: SiteExplorer,
})

/** Sends a signed-in user home; anyone else may see the screen. */
async function redirectHomeWhenSignedIn(context: RouterContext): Promise<void> {
  const session = await context.queryClient.ensureQueryData(sessionQuery(context.api, context.transport))
  if (session !== null) {
    throw redirect({ to: '/' })
  }
}

/** Where an invitation link lands: open to everybody, because whoever holds the link may not have an account yet. */
const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: '/invite', component: AcceptInvitationPage })

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
const routeTree = rootRoute.addChildren([shellRoute.addChildren([homeRoute, siteRoute, membersRoute, auditRoute, adminRoute, settingsRoute, serversRoute]), inviteRoute, loginRoute, setupRoute])

/** Builds the application router; tests pass a memory history. */
export function createAppRouter(context: RouterContext, history?: RouterHistory) {
  return createRouter({ routeTree, context, history })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
