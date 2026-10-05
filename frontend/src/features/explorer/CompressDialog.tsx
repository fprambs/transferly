import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import type { ArchiveFormat, RemoteEntry } from '@/lib/api'

import { archiveName, isArchiveName } from './archive-formats'
import { explorerErrorKeyOf, type ExplorerErrorKey } from './errors'

/** Props of {@link CompressDialog}. */
export interface CompressDialogProps {
  /** The file or folder to pack. */
  entry: RemoteEntry
  /** The formats the server packs, the first one chosen at first. */
  formats: readonly ArchiveFormat[]
  /** Packs the entry into an archive with the chosen name and format; a failure is shown in the dialog. */
  onCompress: (name: string, format: ArchiveFormat) => Promise<void>
  /** Called when the dialog is dismissed or the archive is made. */
  onClose: () => void
}

/**
 * Asks for the format and the name of an archive the server packs a file or folder into, next to it (ADR-0027). The name follows the
 * format until it is edited, and must end with an extension of the format.
 */
export function CompressDialog({ entry, formats, onCompress, onClose }: CompressDialogProps) {
  const { t } = useTranslation('explorer')
  const [format, setFormat] = useState<ArchiveFormat>(formats[0])
  const [name, setName] = useState(archiveName(entry.name, formats[0]))
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<ExplorerErrorKey | 'nameInvalid' | null>(null)

  const choose = (next: ArchiveFormat): void => {
    if (name === archiveName(entry.name, format)) {
      setName(archiveName(entry.name, next))
    }
    setFormat(next)
  }

  const submit = async (event: FormEvent): Promise<void> => {
    event.preventDefault()
    const trimmed = name.trim()
    if (!isArchiveName(trimmed, format)) {
      setFailure('nameInvalid')
      return
    }
    setBusy(true)
    try {
      await onCompress(trimmed, format)
      onClose()
    } catch (error) {
      setFailure(explorerErrorKeyOf(error))
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[440px]">
        <form onSubmit={(event) => void submit(event)} noValidate className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{t(($) => $.compress.title, { name: entry.name })}</DialogTitle>
            <DialogDescription>{t(($) => $.compress.description)}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="archive-format">{t(($) => $.compress.format)}</Label>
            <Select
              id="archive-format"
              value={format}
              options={formats.map((value) => ({ value, label: t(($) => $.compress.formats[value]) }))}
              onValueChange={(value) => choose(value as ArchiveFormat)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="archive-name">{t(($) => $.compress.name)}</Label>
            <Input
              id="archive-name"
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              value={name}
              aria-invalid={failure === 'nameInvalid'}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          {failure !== null && (
            <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
              {failure === 'nameInvalid' ? t(($) => $.compress.nameInvalid, { extension: archiveName('', format) }) : t(($) => $.errors[failure])}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t(($) => $.cancel)}
            </Button>
            <Button type="submit" disabled={busy}>
              {t(($) => $.compress.submit)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
