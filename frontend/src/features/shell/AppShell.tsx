import { useTranslation } from 'react-i18next'

import { LanguageSelect } from './LanguageSelect'
import { QueuePanel } from './QueuePanel'
import { ThemeToggle } from './ThemeToggle'

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
  const { t } = useTranslation()
  const shell = useTranslation('shell').t

  return (
    <div className="flex min-h-svh flex-col">
      <header
        aria-label={shell(($) => $.topbar.label)}
        className="flex flex-wrap items-center justify-between gap-4 border-b px-4 py-2"
      >
        <h1 className="text-lg font-semibold">{t(($) => $.app.name)}</h1>
        <div className="flex items-center gap-4">
          <ThemeToggle />
          <LanguageSelect />
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        <nav aria-label={shell(($) => $.sidebar.label)} className="w-56 border-e bg-card p-4">
          <p className="text-sm text-muted-foreground">{shell(($) => $.sidebar.empty)}</p>
        </nav>
        <main aria-label={shell(($) => $.panes.label)} className="grid flex-1 grid-cols-2 gap-px bg-border">
          <Pane kind="local" />
          <Pane kind="remote" />
        </main>
      </div>
      <QueuePanel />
    </div>
  )
}
