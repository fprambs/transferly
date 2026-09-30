import { useQuery } from '@tanstack/react-query'
import { ImageOff, Info, Maximize, RotateCcw, RotateCw, ZoomIn, ZoomOut } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { PanelState } from '@/components/ui/panel-state'
import { Tip } from '@/components/ui/tip'
import { formatInstant } from '@/lib/datetime'
import { cn } from '@/lib/utils'

import type { PreviewSource } from '../source'
import { readPhotoInfo, type PhotoInfo } from './exif'
import { HexViewer } from './HexViewer'
import { decodeTiff, imageLimit, imageType, isTiff, type Pixels } from './image'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** The zoom steps, as a fraction of the real size. */
const steps = [0.1, 0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4, 8]

/** How the image is sized: fitted to the stage, or at a fraction of its real size. */
type Zoom = 'fit' | number

/** Size of the image, in pixels. */
interface Size {
  /** Width. */
  width: number
  /** Height. */
  height: number
}

/** The metadata fields in the order the panel shows them. */
const infoFields = ['camera', 'taken', 'exposure', 'aperture', 'iso', 'focal', 'software', 'location'] as const

/** Decodes a TIFF for the canvas, or tells it cannot be decoded. */
function tryDecodeTiff(bytes: Uint8Array): Pixels | 'broken' {
  try {
    return decodeTiff(bytes)
  } catch {
    return 'broken'
  }
}

/** The time zone of the browser, in which a camera writes the time a photo was taken. */
function localZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

/** An image already read: drawn by the browser, or decoded onto a canvas for TIFF, with zoom, rotation and its metadata. */
function ImageStage({ source, kind, bytes }: { source: PreviewSource; kind: string; bytes: Uint8Array }) {
  const { t, i18n } = useTranslation('preview')
  const [zoom, setZoom] = useState<Zoom>('fit')
  const [rotation, setRotation] = useState(0)
  const [loaded, setLoaded] = useState<Size | null>(null)
  const [failed, setFailed] = useState(false)
  const [asBytes, setAsBytes] = useState(false)
  const [showInfo, setShowInfo] = useState(false)
  const canvas = useRef<HTMLCanvasElement>(null)
  const tiff = isTiff(bytes)
  const decoded = useMemo(() => (tiff ? tryDecodeTiff(bytes) : null), [tiff, bytes])
  const size = decoded === null || decoded === 'broken' ? loaded : { width: decoded.width, height: decoded.height }
  const broken = failed || decoded === 'broken'
  const url = useMemo(() => (tiff ? null : URL.createObjectURL(new Blob([bytes.slice()], { type: imageType(kind) }))), [bytes, kind, tiff])
  const info = useQuery({ queryKey: ['preview', source.key, 'photo'], queryFn: () => readPhotoInfo(bytes), staleTime: Infinity })

  useEffect(() => () => (url === null ? undefined : URL.revokeObjectURL(url)), [url])

  useEffect(() => {
    if (decoded === null || decoded === 'broken' || canvas.current === null) {
      return
    }
    canvas.current.width = decoded.width
    canvas.current.height = decoded.height
    canvas.current.getContext('2d')?.putImageData(new ImageData(decoded.rgba, decoded.width, decoded.height), 0, 0)
  }, [decoded])

  const current = zoom === 'fit' ? 1 : zoom
  const zoomBy = (direction: 1 | -1): void => {
    const next = direction === 1 ? steps.find((step) => step > current) : [...steps].reverse().find((step) => step < current)
    setZoom(next ?? current)
  }

  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => {
      if (event.ctrlKey || event.metaKey || event.altKey) {
        return
      }
      if (event.key === '+' || event.key === '=') {
        zoomBy(1)
      } else if (event.key === '-') {
        zoomBy(-1)
      } else if (event.key === '0') {
        setZoom('fit')
      } else if (event.key === 'r') {
        setRotation((rotation + 90) % 360)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  })

  if (asBytes) {
    return <HexViewer source={source} kind="binary" />
  }
  if (broken) {
    return (
      <PanelState icon={ImageOff} tone="warn" title={t(($) => $.image.broken)} description={t(($) => $.image.brokenBody)}>
        <Button variant="outline" onClick={() => setAsBytes(true)}>
          {t(($) => $.tooLarge.asBytes)}
        </Button>
      </PanelState>
    )
  }
  const width = size === null || zoom === 'fit' ? undefined : size.width * zoom
  const style = { transform: `rotate(${rotation}deg)`, width }
  const fitted = zoom === 'fit' ? 'max-h-full max-w-full object-contain' : 'max-w-none'
  const details: PhotoInfo = info.data ?? {}

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b px-2 py-1 text-xs">
        <Tip label={t(($) => $.image.zoomOut)} shortcut="-">
          <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.image.zoomOut)} onClick={() => zoomBy(-1)}>
            <ZoomOut />
          </Button>
        </Tip>
        <span className="w-12 text-center tabular-nums text-muted-foreground">{zoom === 'fit' ? '' : `${Math.round(zoom * 100)}%`}</span>
        <Tip label={t(($) => $.image.zoomIn)} shortcut="+">
          <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.image.zoomIn)} onClick={() => zoomBy(1)}>
            <ZoomIn />
          </Button>
        </Tip>
        <Button variant={zoom === 'fit' ? 'secondary' : 'ghost'} size="xs" aria-pressed={zoom === 'fit'} onClick={() => setZoom('fit')}>
          <Maximize />
          {t(($) => $.image.fit)}
        </Button>
        <Button variant={zoom === 1 ? 'secondary' : 'ghost'} size="xs" aria-pressed={zoom === 1} onClick={() => setZoom(1)}>
          {t(($) => $.image.actual)}
        </Button>
        <Tip label={t(($) => $.image.rotateLeft)}>
          <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.image.rotateLeft)} onClick={() => setRotation((rotation + 270) % 360)}>
            <RotateCcw />
          </Button>
        </Tip>
        <Tip label={t(($) => $.image.rotateRight)} shortcut="R">
          <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.image.rotateRight)} onClick={() => setRotation((rotation + 90) % 360)}>
            <RotateCw />
          </Button>
        </Tip>
        {size !== null && <span className="ms-2 tabular-nums text-muted-foreground">{t(($) => $.image.size, { width: size.width, height: size.height })}</span>}
        <Button variant={showInfo ? 'secondary' : 'ghost'} size="xs" className="ms-auto" aria-pressed={showInfo} onClick={() => setShowInfo(!showInfo)}>
          <Info />
          {t(($) => $.image.info)}
        </Button>
      </div>
      <div className="flex min-h-0 flex-1">
        <div
          data-testid="image-stage"
          className="flex min-h-0 flex-1 items-center justify-center overflow-auto bg-[conic-gradient(var(--muted)_25%,transparent_0_50%,var(--muted)_0_75%,transparent_0)] bg-[length:20px_20px] p-4"
        >
          {tiff ? (
            <canvas ref={canvas} role="img" aria-label={source.name} className={cn('transition-transform', fitted)} style={style} />
          ) : (
            <img
              src={url as string}
              alt={source.name}
              className={cn('transition-transform', fitted)}
              style={style}
              onLoad={(event) => setLoaded({ width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight })}
              onError={() => setFailed(true)}
            />
          )}
        </div>
        {showInfo && (
          <aside aria-label={t(($) => $.image.info)} className="w-64 shrink-0 overflow-auto border-s bg-card p-4 text-xs">
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
              <dt className="text-muted-foreground">{t(($) => $.image.fields.dimensions)}</dt>
              <dd className="tabular-nums">{size === null ? '—' : t(($) => $.image.size, { width: size.width, height: size.height })}</dd>
              {infoFields
                .filter((field) => details[field] !== undefined)
                .map((field) => (
                  <div key={field} className="contents">
                    <dt className="text-muted-foreground">{t(($) => $.image.fields[field])}</dt>
                    <dd className="break-words">{field === 'taken' ? formatInstant(details[field] as string, { locale: i18n.language, timeZone: localZone(), dateStyle: 'medium', timeStyle: 'short' }) : details[field]}</dd>
                  </div>
                ))}
            </dl>
            {Object.keys(details).length === 0 && <p className="mt-3 text-muted-foreground">{t(($) => $.image.noMetadata)}</p>}
          </aside>
        )}
      </div>
    </div>
  )
}

/**
 * Shows an image (ADR-0018): the formats the browser draws from a blob URL, SVG as an image so its scripts never run, and TIFF decoded onto a
 * canvas. It zooms (fit, real size and steps, also with + - and 0), rotates (also with R), shows its size and camera metadata, and offers its
 * bytes when it cannot be decoded.
 */
export function ImageViewer({ source, kind }: ViewerProps) {
  return <WholeFile source={source} limit={imageLimit}>{(bytes) => <ImageStage source={source} kind={kind} bytes={bytes} />}</WholeFile>
}
