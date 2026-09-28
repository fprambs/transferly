import { CircleCheck, LoaderCircle } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { LoadingBar } from '@/components/ui/loading-bar'

import { useTransfersStore } from '../transfers/transfers-store'

/** Milliseconds a download stays on screen as started once the server reported it, before it goes away. */
export const startedNoticeMs = 2500

/** A download the user asked for: the identifier the server records it under and the name of what is downloaded. */
export interface RequestedDownload {
  /** Identifier of the transfer the server records. */
  id: string
  /** Name of the file or folder. */
  name: string
}

/** Props of {@link DownloadNotices}. */
export interface DownloadNoticesProps {
  /** The downloads asked for and not dismissed yet. */
  downloads: readonly RequestedDownload[]
  /** Called when a download has been shown as started long enough, to take it away. */
  onDone: (id: string) => void
}

/**
 * Floating cards, one per download asked for, in the corner of the explorer. A card says the download is being prepared, with a spinner and a
 * moving bar, until the server reports it as a transfer; it then says it started and goes away a moment later, so the transfers panel takes over.
 */
export function DownloadNotices({ downloads, onDone }: DownloadNoticesProps) {
  const { t } = useTranslation('explorer')
  const known = useTransfersStore((state) => state.items)
  const started = downloads.filter((download) => known[download.id] !== undefined).map((download) => download.id)
  const startedKey = started.join(',')

  useEffect(() => {
    const timers = startedKey === '' ? [] : startedKey.split(',').map((id) => setTimeout(() => onDone(id), startedNoticeMs))
    return () => timers.forEach(clearTimeout)
  }, [startedKey, onDone])

  if (downloads.length === 0) {
    return null
  }
  return (
    <ul aria-label={t(($) => $.downloads.label)} className="pointer-events-none absolute end-3 bottom-3 z-30 flex w-80 flex-col gap-2">
      {downloads.map((download) => {
        const ready = known[download.id] !== undefined
        return (
          <li
            key={download.id}
            role="status"
            className="pointer-events-auto overflow-hidden rounded-xl border bg-popover text-popover-foreground shadow-lg ring-1 ring-foreground/5 duration-200 animate-in fade-in-0 slide-in-from-bottom-2"
          >
            <div className="flex items-center gap-3 px-3.5 py-3">
              {ready ? (
                <CircleCheck aria-hidden="true" className="size-5 shrink-0 text-emerald-500" />
              ) : (
                <LoaderCircle aria-hidden="true" className="size-5 shrink-0 animate-spin text-accent-text motion-reduce:animate-none" />
              )}
              <div className="flex min-w-0 flex-col">
                <span className="text-sm font-medium">{ready ? t(($) => $.downloads.started) : t(($) => $.downloads.preparing)}</span>
                <span className="truncate font-mono text-xs text-muted-foreground" title={download.name}>
                  {download.name}
                </span>
              </div>
            </div>
            <LoadingBar active={!ready} label={t(($) => $.downloads.preparing)} />
          </li>
        )
      })}
    </ul>
  )
}
