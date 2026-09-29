import { PanelLeftOpen, Server, Zap } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Sheet } from '@/components/ui/sheet'
import { Tip } from '@/components/ui/tip'
import { useDialogsStore } from '@/state/dialogs-store'
import { useUiStore } from '@/state/ui-store'

import { SitesSidebar } from './SitesSidebar'

/** Props of {@link SitesRail}. */
export interface SitesRailProps {
  /** Called with the identifier of the saved site or quick connection the user opens. */
  onOpen: (siteId: string) => void
}

/**
 * What stays of the sites panel while it is hidden, as in VS Code: a narrow rail that shows the panel again, opens the sites in a sheet over
 * the page (closed as soon as one is picked), or starts a quick connection. The files keep the room the panel had.
 */
export function SitesRail({ onOpen }: SitesRailProps) {
  const { t } = useTranslation('shell')
  const [browsing, setBrowsing] = useState(false)
  const setSitesOpen = useUiStore((state) => state.setSitesOpen)
  const setQuickConnectOpen = useDialogsStore((state) => state.setQuickConnectOpen)

  return (
    <nav aria-label={t(($) => $.sidebar.rail)} className="flex h-full flex-col items-center gap-1 bg-card py-2">
      <Tip label={t(($) => $.sidebar.show)} shortcut="Ctrl+B" side="right">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.sidebar.show)} onClick={() => setSitesOpen(true)}>
          <PanelLeftOpen />
        </Button>
      </Tip>
      <Tip label={t(($) => $.sidebar.browse)} side="right">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.sidebar.browse)} className="text-accent-text" onClick={() => setBrowsing(true)}>
          <Server />
        </Button>
      </Tip>
      <Tip label={t(($) => $.sidebar.quickConnect)} side="right">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.sidebar.quickConnect)} className="text-warn" onClick={() => setQuickConnectOpen(true)}>
          <Zap />
        </Button>
      </Tip>
      <Sheet open={browsing} onOpenChange={setBrowsing} title={t(($) => $.sidebar.sites)}>
        <SitesSidebar
          onOpen={(siteId) => {
            setBrowsing(false)
            onOpen(siteId)
          }}
        />
      </Sheet>
    </nav>
  )
}
