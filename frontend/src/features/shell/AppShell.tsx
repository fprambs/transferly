import { Outlet, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { QuickConnectFlow } from '@/features/quick/QuickConnectFlow'
import { TopBar } from '@/features/topbar/TopBar'
import { SitesSidebar } from '@/features/sites/SitesSidebar'

import { QueuePanel } from './QueuePanel'

/** Application layout: top bar, sites sidebar, the explorer of the open site and the transfer queue. */
export function AppShell() {
  const explorer = useTranslation('explorer').t
  const navigate = useNavigate()

  return (
    <div className="flex min-h-svh flex-col">
      <TopBar />
      <div className="flex min-h-0 flex-1">
        <SitesSidebar onOpen={(siteId) => void navigate({ to: '/sites/$siteId', params: { siteId }, search: { path: '/' } })} />
        <main aria-label={explorer(($) => $.label)} className="flex min-w-0 flex-1 flex-col bg-background">
          <Outlet />
        </main>
      </div>
      <QueuePanel />
      <QuickConnectFlow />
    </div>
  )
}
