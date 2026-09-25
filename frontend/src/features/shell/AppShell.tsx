import { useTranslation } from 'react-i18next'

import { QuickConnectFlow } from '@/features/quick/QuickConnectFlow'
import { TopBar } from '@/features/topbar/TopBar'
import { SitesSidebar } from '@/features/sites/SitesSidebar'

import { QueuePanel } from './QueuePanel'

/** Props of {@link Pane}. */
interface PaneProps {
  /** Which side of the dual pane this is. */
  kind: 'local' | 'remote'
}

/** One file pane of the dual pane layout; it shows an empty state until a connection exists. */
function Pane({ kind }: PaneProps) {
  const { t } = useTranslation('shell')

  return (
    <section aria-labelledby={`pane-${kind}`} className="flex flex-col bg-background p-4">
      <h2 id={`pane-${kind}`} className="text-sm font-medium">
        {t(($) => $.panes[kind])}
      </h2>
      <p className="mt-2 text-sm text-muted-foreground">{t(($) => $.panes.empty)}</p>
    </section>
  )
}

/** Application layout: top bar, sites sidebar, dual file pane and transfer queue. */
export function AppShell() {
  const shell = useTranslation('shell').t

  return (
    <div className="flex min-h-svh flex-col">
      <TopBar />
      <div className="flex min-h-0 flex-1">
        <SitesSidebar />
        <main aria-label={shell(($) => $.panes.label)} className="grid flex-1 grid-cols-2 gap-px bg-border">
          <Pane kind="local" />
          <Pane kind="remote" />
        </main>
      </div>
      <QueuePanel />
      <QuickConnectFlow />
    </div>
  )
}
