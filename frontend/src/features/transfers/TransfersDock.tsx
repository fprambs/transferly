import { useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { Transfer, TransferPriority } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'
import { useUiStore } from '@/state/ui-store'

import { useWorkspace } from '../workspace/use-workspace'
import { useTimeZone } from '../auth/use-time-zone'
import { ActivityList } from './ActivityList'
import { TransferRow } from './TransferRow'
import { endedRecently, useNow } from './recent'
import { useResume } from './use-resume'
import { isActive, newestFirst, useTransfersStore, type TransferItem } from './transfers-store'

/** The color of the count of finished transfers: danger when any failed, success when some finished, muted when none. */
function finishedTone(finished: readonly TransferItem[]): string {
  if (finished.some((item) => item.record.status === 'failed' || item.record.status === 'interrupted')) {
    return 'bg-destructive/15 text-destructive'
  }
  return finished.length > 0 ? 'bg-ok/15 text-ok' : 'bg-muted text-muted-foreground'
}

/** A count in a colored badge; its parentheses are kept for assistive technology, so the tab still reads "Active (2)". */
function Count({ value, tone }: { value: number; tone: string }) {
  return (
    <span className={cn('rounded-full px-1.5 font-mono text-[11px] leading-4 tabular-nums', tone)}>
      <span className="sr-only">(</span>
      {value}
      <span className="sr-only">)</span>
    </span>
  )
}

/** Bottom panel that lists the transfers, active ones and finished ones, with their progress and actions. */
export function TransfersDock() {
  const { t } = useTranslation('shell')
  const { t: transfers, i18n } = useTranslation('transfers')
  const workspace = useWorkspace()
  const timeZone = useTimeZone()
  const api = useApi()
  const open = useUiStore((state) => state.queueOpen)
  const toggle = useUiStore((state) => state.toggleQueue)
  const items = useTransfersStore((state) => state.items)
  const forget = useTransfersStore((state) => state.forget)
  const tab = useUiStore((state) => state.queueTab)
  const setTab = useUiStore((state) => state.setQueueTab)
  const picker = useRef<HTMLInputElement>(null)
  const resumer = useResume()
  const failure = resumer.failure
  const clock = useNow(items)
  const { active, finished, shown } = useMemo(() => {
    const all = newestFirst(items)
    return { active: all.filter(isActive), finished: all.filter((item) => !isActive(item)), shown: all.filter((item) => isActive(item) || endedRecently(item, clock)) }
  }, [items, clock])

  const resume = (item: TransferItem): void => {
    const needsFile = resumer.resume(item.record)
    if (needsFile) {
      const input = picker.current as HTMLInputElement
      input.click()
    }
  }

  const cancel = (id: string): void => void api.transfers.cancel(id).catch(() => undefined)
  const apply = (change: Promise<Transfer>): void => void change.catch(() => undefined)
  const pause = (id: string): void => apply(api.transfers.pause(id))
  const proceed = (id: string): void => apply(api.transfers.resume(id))
  const prioritize = (id: string, priority: TransferPriority): void => apply(api.transfers.setPriority(id, priority))
  const remove = (id: string): void => void api.transfers.remove(id).then(() => forget(id)).catch(() => undefined)

  return (
    <section aria-labelledby="queue-title" className="flex h-full min-h-0 flex-col bg-card">
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
      <input
        ref={picker}
        type="file"
        hidden
        aria-label={transfers(($) => $.fileInput)}
        onChange={(event) => {
          resumer.chooseFile((event.target.files as FileList)[0])
          event.target.value = ''
        }}
      />
      {failure === null ? null : (
        <div role="alert" className="mx-4 mb-2 flex items-center justify-between gap-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">
          <span>{transfers(($) => $.resumeErrors[failure])}</span>
          <Button variant="outline" size="sm" onClick={resumer.clearFailure}>
            {transfers(($) => $.resumeErrors.dismiss)}
          </Button>
        </div>
      )}
      {open ? (
        <div id="queue-body" className="flex min-h-0 flex-1 flex-col border-t">
          <Tabs className="min-h-0 flex-1" value={tab} onValueChange={(value) => setTab(value === 'finished' || value === 'activity' ? value : 'active')}>
            <TabsList className="mx-4 mt-2 self-start">
              <TabsTrigger value="active" className="text-foreground">
                {transfers(($) => $.tabs.active)} <Count value={active.length} tone={active.length > 0 ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'} />
              </TabsTrigger>
              <TabsTrigger value="finished" className="text-foreground">
                {transfers(($) => $.tabs.finished)} <Count value={finished.length} tone={finishedTone(finished)} />
              </TabsTrigger>
              <TabsTrigger value="activity" className="text-foreground">
                {transfers(($) => $.tabs.activity)}
              </TabsTrigger>
            </TabsList>
            <TabsContent value="active" className="min-h-0 flex-1 overflow-y-auto">
              {shown.length === 0 ? (
                <p className="px-4 py-3 text-sm text-muted-foreground">{transfers(($) => $.emptyActive)}</p>
              ) : (
                <ul>
                  {shown.map((item) => (
                    <TransferRow key={item.record.id} item={item} onCancel={cancel} onRemove={remove} onResume={resume} onPause={pause} onContinue={proceed} onPriority={prioritize} />
                  ))}
                </ul>
              )}
            </TabsContent>
            <TabsContent value="finished" className="min-h-0 flex-1 overflow-y-auto">
              {finished.length === 0 ? (
                <p className="px-4 py-3 text-sm text-muted-foreground">{transfers(($) => $.emptyFinished)}</p>
              ) : (
                <ul>
                  {finished.map((item) => (
                    <TransferRow key={item.record.id} item={item} onCancel={cancel} onRemove={remove} onResume={resume} onPause={pause} onContinue={proceed} onPriority={prioritize} />
                  ))}
                </ul>
              )}
            </TabsContent>
            <TabsContent value="activity" className="min-h-0 flex-1 overflow-y-auto">
              {workspace !== undefined ? (
                <ActivityList workspaceId={workspace.id} locale={i18n.language} timeZone={timeZone} />
              ) : null}
            </TabsContent>
          </Tabs>
        </div>
      ) : null}
    </section>
  )
}
