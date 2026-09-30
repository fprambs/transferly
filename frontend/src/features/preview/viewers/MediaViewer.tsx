import { MonitorPlay } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/loading'
import { PanelState } from '@/components/ui/panel-state'

import type { MediaAddress } from '../source'
import { HexViewer } from './HexViewer'
import type { ViewerProps } from './types'

/** Playback speeds offered. */
const speeds = [0.5, 1, 1.25, 1.5, 2]

/** Writes a duration in seconds as minutes and seconds, or hours when it is that long. */
function formatDuration(seconds: number): string {
  const whole = Math.round(seconds)
  const hours = Math.floor(whole / 3600)
  const minutes = Math.floor((whole % 3600) / 60)
  const rest = String(whole % 60).padStart(2, '0')
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, '0')}:${rest}` : `${minutes}:${rest}`
}

/** What the player knows once the file's metadata loaded. */
interface Loaded {
  /** Length, in seconds. */
  duration: number
  /** Size of the picture, for video. */
  width: number
  /** Size of the picture, for video. */
  height: number
}

/**
 * Plays audio and video with the player of the browser, which streams the file with ranged requests: a remote file from its download
 * address (range requests record no transfer), a local one from a blob URL (ADR-0018). It offers playback speeds, shows the length and the
 * size of the picture, and says so when the browser cannot play the format, offering its bytes.
 */
export function MediaViewer({ source, kind }: ViewerProps) {
  const { t } = useTranslation('preview')
  const [address, setAddress] = useState<MediaAddress | null>(null)
  const [failed, setFailed] = useState(false)
  const [asBytes, setAsBytes] = useState(false)
  const [speed, setSpeed] = useState(1)
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const player = useRef<HTMLMediaElement | null>(null)

  useEffect(() => {
    let current: MediaAddress | null = null
    let cancelled = false
    void (source.mediaUrl as () => Promise<MediaAddress>)().then((opened) => {
      if (cancelled) {
        opened.release()
        return
      }
      current = opened
      setAddress(opened)
    })
    return () => {
      cancelled = true
      current?.release()
    }
  }, [source])

  const changeSpeed = (next: number): void => {
    setSpeed(next)
    ;(player.current as HTMLMediaElement).playbackRate = next
  }

  if (asBytes) {
    return <HexViewer source={source} kind="binary" />
  }
  if (failed) {
    return (
      <PanelState icon={MonitorPlay} tone="warn" title={t(($) => $.media.unsupported)} description={t(($) => $.media.unsupportedBody)}>
        <Button variant="outline" onClick={() => setAsBytes(true)}>
          {t(($) => $.tooLarge.asBytes)}
        </Button>
      </PanelState>
    )
  }
  if (address === null) {
    return <Loading label={t(($) => $.loading)} className="m-auto" />
  }
  const common = {
    ref: (element: HTMLMediaElement | null) => {
      player.current = element
    },
    src: address.url,
    controls: true,
    preload: 'metadata',
    'aria-label': source.name,
    onError: () => setFailed(true),
    onLoadedMetadata: (event: React.SyntheticEvent<HTMLMediaElement>) => {
      const media = event.currentTarget as HTMLVideoElement
      media.playbackRate = speed
      setLoaded({ duration: media.duration, width: media.videoWidth ?? 0, height: media.videoHeight ?? 0 })
    },
  } as const
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b px-2 py-1 text-xs">
        <span className="me-1 text-muted-foreground">{t(($) => $.media.speed)}</span>
        {speeds.map((option) => (
          <Button key={option} variant={speed === option ? 'secondary' : 'ghost'} size="xs" aria-pressed={speed === option} onClick={() => changeSpeed(option)}>
            {option}×
          </Button>
        ))}
        {loaded !== null && (
          <span className="ms-auto tabular-nums text-muted-foreground">
            {Number.isFinite(loaded.duration) ? formatDuration(loaded.duration) : ''}
            {loaded.width > 0 && ` · ${t(($) => $.image.size, { width: loaded.width, height: loaded.height })}`}
          </span>
        )}
      </div>
      <div className="flex min-h-0 flex-1 items-center justify-center bg-black/90 p-4">
        {kind === 'video' ? <video {...common} className="max-h-full max-w-full" /> : <audio {...common} className="w-full max-w-xl" />}
      </div>
    </div>
  )
}
