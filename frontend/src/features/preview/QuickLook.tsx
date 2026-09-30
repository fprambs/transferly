import { ChevronLeft, ChevronRight, Download, X } from 'lucide-react'
import { useRef, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Tip } from '@/components/ui/tip'

import { formatSize } from '../explorer/entries'
import { FileTypeIcon } from '../explorer/FileTypeIcon'
import { fileTypeOf } from '../explorer/file-types'

import { PreviewBody } from './PreviewBody'
import type { PreviewSource } from './source'

/** Props of {@link QuickLook}. */
export interface QuickLookProps {
  /** The files of the folder, in the order the arrows walk them. */
  sources: readonly PreviewSource[]
  /** The file shown, or `null` when the viewer is closed. */
  index: number | null
  /** Shows another file of the folder. */
  onIndexChange: (index: number) => void
  /** Closes the viewer. */
  onClose: () => void
  /** Downloads the file shown; the button is left out without it. */
  onDownload?: (source: PreviewSource) => void
  /** Opens the file shown in the editor, offered for text; the button is left out without it. */
  onEdit?: (source: PreviewSource) => void
}

/**
 * Shows a file inside Sentinel without downloading it, full screen in the style of Quick Look (ADR-0018). The arrows move to the previous
 * and next file of the folder and Escape closes; the bar names the file and offers to download it. The viewer is chosen by what the file
 * holds, read from its first bytes. It opens with the focus on the file, not on a button, so the first Escape closes it. A viewer with a text field of its own stops the arrow keys typed in it from reaching the viewer.
 */
export function QuickLook({ sources, index, onIndexChange, onClose, onDownload, onEdit }: QuickLookProps) {
  const { t } = useTranslation('preview')
  const source = index === null ? undefined : sources[index]
  const body = useRef<HTMLDivElement>(null)
  const go = (from: number, step: number): void => {
    if (sources[from + step] !== undefined) {
      onIndexChange(from + step)
    }
  }
  const onKeyDown =
    index === null
      ? undefined
      : (event: KeyboardEvent): void => {
          if (event.altKey || event.ctrlKey || event.metaKey) {
            return
          }
          if (event.key === 'ArrowLeft') {
            go(index, -1)
          } else if (event.key === 'ArrowRight') {
            go(index, 1)
          }
        }
  return (
    <Dialog open={source !== undefined} onOpenChange={() => onClose()}>
      <DialogContent showCloseButton={false} className="flex h-dvh w-screen max-w-none flex-col gap-0 rounded-none p-0 sm:max-w-none"
        initialFocus={body}
        onKeyDown={onKeyDown}
      >
        {source !== undefined && index !== null && (
          <>
            <header className="flex items-center gap-3 border-b px-4 py-2.5">
              <FileTypeIcon type={fileTypeOf({ name: source.name, type: 'file' })} className="size-5 shrink-0" />
              <div className="min-w-0 flex-1">
                <DialogTitle className="truncate text-sm font-medium">{source.name}</DialogTitle>
                <DialogDescription className="text-xs text-muted-foreground">
                  {formatSize(source.size)} · {t(($) => $.position, { index: index + 1, count: sources.length })}
                </DialogDescription>
              </div>
              <Tip label={t(($) => $.previous)}>
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.previous)} disabled={index === 0} onClick={() => go(index, -1)}>
                  <ChevronLeft />
                </Button>
              </Tip>
              <Tip label={t(($) => $.next)}>
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.next)} disabled={index === sources.length - 1} onClick={() => go(index, 1)}>
                  <ChevronRight />
                </Button>
              </Tip>
              {onDownload !== undefined && (
                <Tip label={t(($) => $.download)}>
                  <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.download)} onClick={() => onDownload(source)}>
                    <Download />
                  </Button>
                </Tip>
              )}
              <Tip label={t(($) => $.close)} shortcut="Esc">
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.close)} onClick={onClose}>
                  <X />
                </Button>
              </Tip>
            </header>
            <div ref={body} tabIndex={-1} className="flex min-h-0 flex-1 flex-col outline-none">
              <PreviewBody key={source.key} source={source} onEdit={onEdit} />
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
