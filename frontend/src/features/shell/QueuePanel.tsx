import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useUiStore } from '@/state/ui-store'

/** Collapsible panel that lists the transfer queue. */
export function QueuePanel() {
  const { t } = useTranslation('shell')
  const open = useUiStore((state) => state.queueOpen)
  const toggle = useUiStore((state) => state.toggleQueue)

  return (
    <section aria-labelledby="queue-title" className="border-t bg-card">
      <div className="flex items-center justify-between px-4 py-2">
        <h2 id="queue-title" className="text-sm font-medium">
          {t(($) => $.queue.title)}
        </h2>
        <Button
          size="sm"
          variant="ghost"
          aria-expanded={open}
          aria-controls="queue-body"
          onClick={toggle}
        >
          {open ? t(($) => $.queue.hide) : t(($) => $.queue.show)}
        </Button>
      </div>
      {open && (
        <p id="queue-body" className="px-4 pb-3 text-sm text-muted-foreground">
          {t(($) => $.queue.empty)}
        </p>
      )}
    </section>
  )
}
