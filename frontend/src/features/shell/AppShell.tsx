import { Outlet, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { QuickConnectFlow } from '@/features/quick/QuickConnectFlow'
import { TopBar } from '@/features/topbar/TopBar'
import { SitesSidebar } from '@/features/sites/SitesSidebar'
import { TransfersDock } from '@/features/transfers/TransfersDock'
import { useAccountTheme } from '@/features/settings/use-account-theme'
import { useTransferStream } from '@/features/transfers/use-transfer-stream'


/** Application layout: top bar, sites sidebar, the explorer of the open site and the transfer queue. */
export function AppShell() {
  const explorer = useTranslation('explorer').t
  const navigate = useNavigate()
  useTransferStream()
  useAccountTheme()

  return (
    <div className="flex h-svh flex-col overflow-hidden">
      <TopBar />
      <div className="flex min-h-0 flex-1">
        <SitesSidebar onOpen={(siteId) => void navigate({ to: '/sites/$siteId', params: { siteId }, search: { path: '/' } })} />
        <main aria-label={explorer(($) => $.label)} className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto bg-background">
          <Outlet />
        </main>
      </div>
      <TransfersDock />
      <QuickConnectFlow />
    </div>
  )
}
