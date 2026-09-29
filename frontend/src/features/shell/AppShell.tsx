import { Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { Group, layoutStorage, Panel, ResizeHandle, useCollapsible, useDefaultLayout } from '@/components/ui/resizable'
import { useUiStore } from '@/state/ui-store'

import { QuickConnectFlow } from '@/features/quick/QuickConnectFlow'
import { Toolbar } from '@/features/toolbar/Toolbar'
import { useConnectionState } from '@/features/toolbar/connection-store'
import { TopBar } from '@/features/topbar/TopBar'
import { SitesRail } from '@/features/sites/SitesRail'
import { SitesSidebar } from '@/features/sites/SitesSidebar'
import { TransfersDock } from '@/features/transfers/TransfersDock'
import { useAccountTheme } from '@/features/settings/use-account-theme'
import { useTransferStream } from '@/features/transfers/use-transfer-stream'

/** Height of the transfers panel while collapsed: its header, which keeps the button that opens it again. */
const transfersHeaderPx = 45

/** Width of the sites panel while hidden: the rail that brings it back. */
const railPx = 48

/** Whether a key press happens while typing, where Ctrl+B belongs to the field. */
function typing(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
}

/**
 * Application layout: top bar, sites panel, the toolbar above the explorer (on the start page and the sites, not on settings or
 * administration), and the transfer queue. The sites and transfers panels are resizable, remember their sizes in the browser, and collapse
 * as the preferences say (4.10); the hidden sites panel leaves a rail, and Ctrl+B shows or hides it. Opening a site from the sidebar connects it again if the user had disconnected it.
 */
export function AppShell() {
  const { t: explorer } = useTranslation('explorer')
  const { t: shell } = useTranslation('shell')
  const navigate = useNavigate()
  const reconnectSite = useConnectionState((state) => state.reconnectSite)
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const explorerView = pathname === '/' || pathname.startsWith('/sites/')
  const sitesOpen = useUiStore((state) => state.sitesOpen)
  const setSitesOpen = useUiStore((state) => state.setSitesOpen)
  const queueOpen = useUiStore((state) => state.queueOpen)
  const setQueueOpen = useUiStore((state) => state.setQueueOpen)
  const sites = useCollapsible(sitesOpen, setSitesOpen)
  const transfers = useCollapsible(queueOpen, setQueueOpen)
  const outer = useDefaultLayout({ id: 'sentinel-shell', storage: layoutStorage })
  const inner = useDefaultLayout({ id: 'sentinel-work', storage: layoutStorage })
  useTransferStream()
  useAccountTheme()

  useEffect(() => {
    const toggle = (event: KeyboardEvent): void => {
      if ((event.ctrlKey || event.metaKey) && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 'b' && !typing(event.target)) {
        event.preventDefault()
        setSitesOpen(!useUiStore.getState().sitesOpen)
      }
    }
    window.addEventListener('keydown', toggle)
    return () => window.removeEventListener('keydown', toggle)
  }, [setSitesOpen])

  const open = (siteId: string): void => {
    reconnectSite(siteId)
    void navigate({ to: '/sites/$siteId', params: { siteId }, search: { path: '/' } })
  }

  return (
    <div className="flex h-svh flex-col overflow-hidden">
      <TopBar />
      <Group orientation="vertical" id="sentinel-shell" className="min-h-0 flex-1" {...outer}>
        <Panel id="work" minSize={160}>
          <Group id="sentinel-work" className="h-full" {...inner}>
            <Panel id="sites" collapsible collapsedSize={railPx} defaultSize={248} minSize={180} maxSize="45%" groupResizeBehavior="preserve-pixel-size" className="h-full" {...sites}>
              {sitesOpen ? <SitesSidebar onOpen={open} onHide={() => setSitesOpen(false)} /> : <SitesRail onOpen={open} />}
            </Panel>
            <ResizeHandle aria-label={shell(($) => $.panels.sites)} />
            <Panel id="main" minSize="30%" className="h-full">
              <main aria-label={explorer(($) => $.label)} className="flex h-full min-h-0 min-w-0 flex-col overflow-y-auto bg-background">
                {explorerView && <Toolbar />}
                <Outlet />
              </main>
            </Panel>
          </Group>
        </Panel>
        <ResizeHandle aria-label={shell(($) => $.panels.transfers)} />
        <Panel id="transfers" collapsible collapsedSize={transfersHeaderPx} defaultSize={230} minSize={96} maxSize="70%" groupResizeBehavior="preserve-pixel-size" className="h-full" {...transfers}>
          <TransfersDock />
        </Panel>
      </Group>
      <QuickConnectFlow />
    </div>
  )
}
