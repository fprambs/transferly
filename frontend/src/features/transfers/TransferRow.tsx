import { ArrowDown, ArrowLeftRight, ArrowUp, Pause, Play, RotateCw, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import type { TransferPriority } from '@/lib/api'
import { cn } from '@/lib/utils'
import { formatSize } from '../explorer/entries'

import { canResume } from './use-resume'
import { isStoppedUpload } from './resume'
import { formatDuration, formatSpeed, percentDone } from './format'
import { etaSeconds, isActive, type TransferItem } from './transfers-store'

/** Props of {@link TransferRow}. */
export interface TransferRowProps {
  /** The transfer to show. */
  item: TransferItem
  /** Called to cancel a transfer that is queued or running. */
  onCancel: (id: string) => void
  /** Called to remove a finished transfer from the list. */
  onRemove: (id: string) => void
  /** Called to resume an upload that stopped. */
  onResume: (item: TransferItem) => void
  /** Called to hold a queued transfer back until it is continued. */
  onPause: (id: string) => void
  /** Called to let a paused, queued transfer start when a slot is free. */
  onContinue: (id: string) => void
  /** Called to change the place of a queued transfer in the line. */
  onPriority: (id: string, priority: TransferPriority) => void
}

/** The priorities a queued transfer can be given, in the order they are offered. */
const priorities = [
  { value: 1, key: 'high' },
  { value: 0, key: 'normal' },
  { value: -1, key: 'low' },
] as const

/** The icon of each direction of a transfer. */
const directionIcons = { upload: ArrowUp, download: ArrowDown, copy: ArrowLeftRight } as const

/** The color of each direction, so uploads and downloads tell apart at a glance. */
const directionTones = { upload: 'text-accent-text', download: 'text-info', copy: 'text-warn' } as const

/** The color of the state of a finished transfer: success, failure, or a neutral stop. */
const finishedTones: Record<TransferItem['record']['status'], string> = {
  queued: '',
  running: '',
  done: 'text-ok',
  failed: 'text-destructive',
  interrupted: 'text-destructive',
  canceled: '',
}

/** The fill of the progress bar of an active transfer: striped brand color while it moves, warning when held, muted when waiting. */
function barTone(running: boolean, paused: boolean): string {
  if (running) {
    return 'progress-stripes bg-primary'
  }
  return paused ? 'bg-warn' : 'bg-muted-foreground/40'
}

/** One transfer of the dock: what it moves, how far it got, how fast, and the action that fits its state. */
export function TransferRow({ item, onCancel, onRemove, onResume, onPause, onContinue, onPriority }: TransferRowProps) {
  const { t } = useTranslation('transfers')
  const { record } = item
  const active = isActive(item)
  const percent = percentDone(record.bytesDone, record.totalBytes)
  const eta = etaSeconds(item)
  const queued = record.status === 'queued'
  const paused = record.paused === true
  const Icon = directionIcons[record.direction]

  return (
    <li className="flex items-center gap-3 border-b px-4 py-2 text-[13px] last:border-b-0">
      <Icon aria-label={t(($) => $[record.direction])} className={cn('size-4 shrink-0', directionTones[record.direction])} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-baseline gap-2">
          <span className="truncate font-medium">{record.name}</span>
          <span className="truncate text-xs text-muted-foreground">{record.siteName}</span>
        </div>
        {active ? (
          <div
            role="progressbar"
            aria-label={t(($) => $.progress, { name: record.name })}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percent}
            className="h-1.5 overflow-hidden rounded-full bg-muted"
          >
            <div className={cn('h-full rounded-full transition-[width]', barTone(record.status === 'running', paused))} style={{ width: `${percent ?? 0}%` }} />
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-x-3 font-mono text-[11px] text-muted-foreground">
          <span>
            {record.totalBytes === undefined
              ? t(($) => $.unknownSize, { done: formatSize(record.bytesDone) })
              : t(($) => $.of, { done: formatSize(record.bytesDone), total: formatSize(record.totalBytes) })}
          </span>
          {record.status === 'running' && item.speed > 0 ? <span>{formatSpeed(item.speed)}</span> : null}
          {record.status === 'running' && eta !== undefined ? <span>{t(($) => $.left, { time: formatDuration(eta) })}</span> : null}
          {queued ? <span>{paused ? t(($) => $.paused) : t(($) => $.waiting)}</span> : null}
          {active ? null : (
            <span className={cn('font-medium', finishedTones[record.status])}>
              {t(($) => $.status[record.status])}
              {record.errorCode === undefined ? '' : ` · ${record.errorCode}`}
            </span>
          )}
        </div>
      </div>
      {active ? (
        <div className="flex items-center gap-1">
          {queued ? (
            <>
              <Select
                aria-label={t(($) => $.priority.label, { name: record.name })}
                className="h-7 w-28 text-xs"
                value={String(record.priority ?? 0)}
                options={priorities.map((priority) => ({ value: String(priority.value), label: t(($) => $.priority[priority.key]) }))}
                onValueChange={(value) => onPriority(record.id, Number(value) as TransferPriority)}
              />
              {paused ? (
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.resumeQueuedLabel, { name: record.name })} onClick={() => onContinue(record.id)}>
                  <Play />
                </Button>
              ) : (
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.pauseLabel, { name: record.name })} onClick={() => onPause(record.id)}>
                  <Pause />
                </Button>
              )}
            </>
          ) : null}
          <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.cancelLabel, { name: record.name })} onClick={() => onCancel(record.id)}>
            <X />
          </Button>
        </div>
      ) : (
        <div className="flex items-center gap-1">
          {isStoppedUpload(record) && canResume(record) ? (
            <Button variant="outline" size="sm" aria-label={t(($) => $.resumeLabel, { name: record.name })} onClick={() => onResume(item)}>
              <RotateCw />
              {t(($) => $.resume)}
            </Button>
          ) : null}
          <Button variant="ghost" size="sm" aria-label={t(($) => $.removeLabel, { name: record.name })} onClick={() => onRemove(record.id)}>
            {t(($) => $.remove)}
          </Button>
        </div>
      )}
    </li>
  )
}
