import { Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { QuickConnectFlow } from '@/features/quick/QuickConnectFlow'
import { Toolbar } from '@/features/toolbar/Toolbar'
import { useConnectionState } from '@/features/toolbar/connection-store'
import { TopBar } from '@/features/topbar/TopBar'
import { SitesSidebar } from '@/features/sites/SitesSidebar'
import { TransfersDock } from '@/features/transfers/TransfersDock'
import { useAccountTheme } from '@/features/settings/use-account-theme'
import { useTransferStream } from '@/features/transfers/use-transfer-stream'


/**
 * Application layout: top bar, sites sidebar, the toolbar above the explorer (on the start page and the sites, not on settings or
 * administration), and the transfer queue. Opening a site from the sidebar connects it again if the user had disconnected it.
 */
export function AppShell() {
  const explorer = useTranslation('explorer').t
  const navigate = useNavigate()
  const reconnectSite = useConnectionState((state) => state.reconnectSite)
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const explorerView = pathname === '/' || pathname.startsWith('/sites/')
  useTransferStream()
  useAccountTheme()

  return (
    <div className="flex h-svh flex-col overflow-hidden">
      <TopBar />
      <div className="flex min-h-0 flex-1">
        <SitesSidebar
          onOpen={(siteId) => {
            reconnectSite(siteId)
            void navigate({ to: '/sites/$siteId', params: { siteId }, search: { path: '/' } })
          }}
        />
        <main aria-label={explorer(($) => $.label)} className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto bg-background">
          {explorerView && <Toolbar />}
          <Outlet />
        </main>
      </div>
      <TransfersDock />
      <QuickConnectFlow />
    </div>
  )
}
