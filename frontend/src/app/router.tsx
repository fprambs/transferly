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

import { AcceptInvitationPage } from '@/features/teams/AcceptInvitationPage'
import { settingsSections, type SettingsSection } from '@/features/settings/sections'
import { NotFound } from '@/features/shell/NotFound'
import { RouteFault } from '@/features/shell/RouteFault'
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
const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
  errorComponent: RouteFault,
})

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
  validateSearch: z.object({ settings: z.enum(settingsSections).optional().catch(undefined) }),
  component: AppShell,
})

/** Home of a signed-in user: no site is open yet. */
const homeRoute = createRoute({ getParentRoute: () => shellRoute, path: '/', component: EmptyExplorer })

/**
 * An address of the time settings, administration and the people of a workspace were pages of their own: it now opens the section of the
 * settings dialog over the start page, so old bookmarks and links keep working.
 */
function legacySection(path: string, section: SettingsSection) {
  return createRoute({
    getParentRoute: () => shellRoute,
    path,
    beforeLoad: () => {
      throw redirect({ to: '/', search: { settings: section } })
    },
  })
}

/** Former page of the settings of the interface. */
const settingsRoute = legacySection('/settings', 'general')

/** Former page of the members and invitations of the team workspace in use. */
const membersRoute = legacySection('/members', 'members')

/** Former page of the audit trail of the team workspace in use. */
const auditRoute = legacySection('/audit', 'audit')

/** Former page of the administration of the instance. */
const adminRoute = legacySection('/admin', 'admin')

/** Former page of the servers whose key the user trusted. */
const serversRoute = legacySection('/servers', 'servers')

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
