import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useApi } from '@/lib/api/use-api'
import { useUiStore } from '@/state/ui-store'

import { TransferRow } from './TransferRow'
import { isActive, newestFirst, useTransfersStore } from './transfers-store'

/** Bottom panel that lists the transfers, active ones and finished ones, with their progress and actions. */
export function TransfersDock() {
  const { t } = useTranslation('shell')
  const { t: transfers } = useTranslation('transfers')
  const api = useApi()
  const open = useUiStore((state) => state.queueOpen)
  const toggle = useUiStore((state) => state.toggleQueue)
  const items = useTransfersStore((state) => state.items)
  const forget = useTransfersStore((state) => state.forget)
  const [tab, setTab] = useState('active')
  const { active, finished } = useMemo(() => {
    const all = newestFirst(items)
    return { active: all.filter(isActive), finished: all.filter((item) => !isActive(item)) }
  }, [items])

  const cancel = (id: string): void => void api.transfers.cancel(id).catch(() => undefined)
  const remove = (id: string): void => void api.transfers.remove(id).then(() => forget(id)).catch(() => undefined)

  return (
    <section aria-labelledby="queue-title" className="border-t bg-card">
      <div className="flex items-center justify-between px-4 py-2">
        <div className="flex items-baseline gap-3">
          <h2 id="queue-title" className="text-sm font-medium">
            {t(($) => $.queue.title)}
          </h2>
          {active.length > 0 ? <span className="font-mono text-xs text-muted-foreground">{transfers(($) => $.summary, { count: active.length })}</span> : null}
        </div>
        <Button size="sm" variant="ghost" aria-expanded={open} aria-controls="queue-body" onClick={toggle}>
          {open ? t(($) => $.queue.hide) : t(($) => $.queue.show)}
        </Button>
      </div>
      {open ? (
        <div id="queue-body" className="border-t">
          {Object.keys(items).length === 0 ? (
            <p className="px-4 py-3 text-sm text-muted-foreground">{t(($) => $.queue.empty)}</p>
          ) : (
            <Tabs value={tab} onValueChange={(value) => setTab(String(value))}>
              <TabsList className="mx-4 mt-2 self-start">
                <TabsTrigger value="active">
                  {transfers(($) => $.tabs.active)} ({active.length})
                </TabsTrigger>
                <TabsTrigger value="finished">
                  {transfers(($) => $.tabs.finished)} ({finished.length})
                </TabsTrigger>
              </TabsList>
              <TabsContent value="active" className="max-h-60 overflow-y-auto">
                {active.length === 0 ? (
                  <p className="px-4 py-3 text-sm text-muted-foreground">{transfers(($) => $.emptyActive)}</p>
                ) : (
                  <ul>
                    {active.map((item) => (
                      <TransferRow key={item.record.id} item={item} onCancel={cancel} onRemove={remove} />
                    ))}
                  </ul>
                )}
              </TabsContent>
              <TabsContent value="finished" className="max-h-60 overflow-y-auto">
                {finished.length === 0 ? (
                  <p className="px-4 py-3 text-sm text-muted-foreground">{transfers(($) => $.emptyFinished)}</p>
                ) : (
                  <ul>
                    {finished.map((item) => (
                      <TransferRow key={item.record.id} item={item} onCancel={cancel} onRemove={remove} />
                    ))}
                  </ul>
                )}
              </TabsContent>
            </Tabs>
          )}
        </div>
      ) : null}
    </section>
  )
}
