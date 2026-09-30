import { ArrowRight } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Loading } from '@/components/ui/loading'
import { formatInstant } from '@/lib/datetime'
import { useApi } from '@/lib/api/use-api'

import { useTimeZone } from '../auth/use-time-zone'
import { formatSize } from '../explorer/entries'

import type { SyncErrorKey } from './errors'
import { pending, type SyncPlanRow } from './plan'
import type { SyncSide } from './sides'
import { useSyncPlan, type SyncDirection } from './use-sync'

/** Props of {@link CompareDialog}. */
export interface CompareDialogProps {
  /** One side of the comparison. */
  left: SyncSide
  /** The other side. */
  right: SyncSide
  /** Name shown for the left side. */
  leftLabel: string
  /** Name shown for the right side. */
  rightLabel: string
  /** Called when the dialog is dismissed. */
  onClose: () => void
}

/** Colors of the badge of each action, distinct in hue and lightness so they can be told apart without color. */
const actionTones: Readonly<Record<SyncPlanRow['action'], string>> = {
  create: 'bg-emerald-500/15 text-emerald-800 dark:text-emerald-300',
  update: 'bg-sky-500/15 text-sky-800 dark:text-sky-300',
  delete: 'bg-red-500/15 text-red-800 dark:text-red-300',
}

/** Props of {@link PlanRow}. */
interface PlanRowProps {
  /** The row to draw. */
  row: SyncPlanRow
  /** The label of the side the row's files are shown for; used only to word the size and time shown. */
  entry: SyncPlanRow['source'] | SyncPlanRow['target']
  /** Time zone the modification time is shown in. */
  timeZone: string
  /** Locale the modification time is formatted in. */
  locale: string
  /** Checks or unchecks the row. */
  onToggle: () => void
  /** Reads the checksum of the row on both sides, when offered. */
  onVerify: () => void
  /** Whether a checksum of this row is being read. */
  verifying: boolean
}

/** One row of the plan: what will happen to a file, why, and its status once the plan runs. */
function PlanRow({ row, entry, timeZone, locale, onToggle, onVerify, verifying }: PlanRowProps) {
  const { t } = useTranslation('sync')
  const offerVerify = row.action === 'update' && row.reason !== 'size'

  return (
    <li className="grid grid-cols-[auto_1.2fr_2.2fr_1fr] items-center gap-3 border-b px-4 py-2.5 text-[13px] last:border-b-0">
      <Checkbox aria-label={t(($) => $.row.include, { path: row.relativePath })} checked={row.included} onCheckedChange={onToggle} />
      <span className={`inline-flex h-[22px] w-fit items-center rounded-full px-2.5 text-xs font-medium ${actionTones[row.action]}`}>{t(($) => $.action[row.action])}</span>
      <span className="min-w-0">
        <span className="block truncate font-mono">{row.relativePath}</span>
        <span className="block text-xs text-muted-foreground">
          {t(($) => $.row.reason[row.reason])}
          {row.newerTarget && <span className="font-medium text-warn"> · {t(($) => $.row.newerTarget)}</span>}
          {entry !== null && ` · ${formatSize(entry.size)} · ${formatInstant(entry.modifiedAt, { locale, timeZone, dateStyle: 'medium', timeStyle: 'short' })}`}
        </span>
      </span>
      <span className="flex items-center justify-end gap-2">
        {row.status === 'running' && <span className="text-xs text-muted-foreground">{t(($) => $.row.status.running)}</span>}
        {row.status === 'done' && <span className="text-xs text-emerald-700 dark:text-emerald-400">{t(($) => $.row.status.done)}</span>}
        {row.status === 'failed' && <span className="text-xs text-destructive">{t(($) => $.errors[row.error as SyncErrorKey])}</span>}
        {row.status === 'pending' && offerVerify && (
          <Button variant="outline" size="sm" disabled={verifying} onClick={onVerify}>
            {verifying ? t(($) => $.row.verifying) : t(($) => $.row.verify)}
          </Button>
        )}
      </span>
    </li>
  )
}

/** Compares two folders and lets a person review and run a sync between them, mirroring one onto the other (ADR-0008). */
export function CompareDialog({ left, right, leftLabel, rightLabel, onClose }: CompareDialogProps) {
  const { t } = useTranslation('sync')
  const api = useApi()
  const timeZone = useTimeZone()
  const { i18n } = useTranslation()
  const plan = useSyncPlan(api, left, right)
  const [confirming, setConfirming] = useState(false)
  const checked = plan.rows.filter(pending)
  const failure = plan.loadFailure
  const sourceLabel = plan.direction === 'leftToRight' ? leftLabel : rightLabel
  const targetLabel = plan.direction === 'leftToRight' ? rightLabel : leftLabel

  const start = (): void => {
    if ((plan.hasDeletes || plan.hasOverwrites) && !confirming) {
      setConfirming(true)
      return
    }
    setConfirming(false)
    void plan.run()
  }

  const directionButton = (direction: SyncDirection, from: string, to: string): ReactNode => (
    <Button variant={plan.direction === direction ? 'default' : 'outline'} size="sm" onClick={() => plan.setDirection(direction)} disabled={plan.running}>
      {from}
      {' '}
      <ArrowRight className="size-3.5" />
      {' '}
      {to}
    </Button>
  )

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[720px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.subtitle, { source: sourceLabel, target: targetLabel })}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            {directionButton('leftToRight', leftLabel, rightLabel)}
            {directionButton('rightToLeft', rightLabel, leftLabel)}
          </div>
          <Button variant="ghost" size="sm" onClick={plan.refresh} disabled={plan.loading || plan.running}>
            {t(($) => $.refresh)}
          </Button>
        </div>
        {plan.loading ? (
          <Loading label={t(($) => $.loading)} className="justify-center py-6" />
        ) : failure !== null ? (
          <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {t(($) => $.errors[failure])}
          </p>
        ) : plan.rows.length === 0 ? (
          <p className="rounded-lg border bg-card px-4 py-6 text-center text-sm text-muted-foreground">{t(($) => $.empty)}</p>
        ) : (
          <div className="max-h-[360px] overflow-y-auto overflow-x-hidden rounded-lg border bg-card">
            <ul aria-label={t(($) => $.list)}>
              {plan.rows.map((row) => (
                <PlanRow
                  key={`${row.action}:${row.relativePath}`}
                  row={row}
                  entry={row.action === 'delete' ? row.target : row.source}
                  timeZone={timeZone}
                  locale={i18n.language}
                  onToggle={() => plan.toggle(row)}
                  onVerify={() => void plan.verify(row)}
                  verifying={plan.verifying === row.relativePath}
                />
              ))}
            </ul>
          </div>
        )}
        {confirming && (
          <div role="alert" className="flex flex-col gap-1 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2.5 text-sm leading-relaxed text-destructive">
            {plan.hasDeletes && <p>{t(($) => $.confirmDeletes, { count: checked.filter((row) => row.action === 'delete').length, target: targetLabel })}</p>}
            {plan.hasOverwrites && <p>{t(($) => $.confirmOverwrites, { count: checked.filter((row) => row.newerTarget).length, target: targetLabel })}</p>}
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.close)}
          </Button>
          <Button variant={confirming ? 'destructive' : 'default'} disabled={plan.loading || plan.running || checked.length === 0} onClick={start}>
            {confirming ? (plan.hasDeletes ? t(($) => $.confirmRun) : t(($) => $.confirmOverwrite)) : t(($) => $.run, { count: checked.length })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
