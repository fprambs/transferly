import { FileLock2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Loading } from '@/components/ui/loading'
import { useApi } from '@/lib/api/use-api'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'

/** Props of {@link ExportDialog}. */
export interface ExportDialogProps {
  /** Whether the dialog is open. */
  open: boolean
  /** Workspace whose sites are exported. */
  workspaceId: string
  /** Called when the dialog is dismissed. */
  onClose: () => void
  /** Saves the file; the browser download by default, replaceable in tests. */
  save?: (file: Blob, name: string) => void
}

/** The shortest password the server takes for an export. */
const minPasswordLength = 12

/** Hands a file to the browser as a download named `name`. */
function download(file: Blob, name: string): void {
  const url = URL.createObjectURL(file)
  const link = document.createElement('a')
  link.href = url
  link.download = name
  link.click()
  URL.revokeObjectURL(url)
}

/** Names the file of an export after the day it was made, such as `sentinel-sites-2026-09-29.json`. */
function exportFileName(now: Date): string {
  return `sentinel-sites-${now.toISOString().slice(0, 10)}.json`
}

/** Why an export could not be made. */
type Failure = SiteErrorKey | 'mismatch' | 'weak'

/** The form inside {@link ExportDialog}, mounted fresh on every opening so no password lingers. */
function ExportForm({ workspaceId, onClose, save = download }: Omit<ExportDialogProps, 'open'>) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [failure, setFailure] = useState<Failure | null>(null)
  const [working, setWorking] = useState(false)
  const [saved, setSaved] = useState<string | null>(null)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    event.stopPropagation()
    if ([...password].length < minPasswordLength) {
      setFailure('weak')
      return
    }
    if (password !== confirm) {
      setFailure('mismatch')
      return
    }
    setWorking(true)
    setFailure(null)
    try {
      const name = exportFileName(new Date())
      save(await api.exports.create(workspaceId, password), name)
      setSaved(name)
    } catch (error) {
      setFailure(siteErrorKeyOf(error))
    } finally {
      setWorking(false)
    }
  }

  return (
    <form onSubmit={(event) => void submit(event)} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>{t(($) => $.export.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.export.description)}</DialogDescription>
      </DialogHeader>
      {saved === null ? (
        <>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="export-password">{t(($) => $.export.password)}</Label>
            <Input id="export-password" type="password" autoFocus autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} />
            <p className="text-xs text-muted-foreground">{t(($) => $.export.hint)}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="export-confirm">{t(($) => $.export.confirm)}</Label>
            <Input id="export-confirm" type="password" autoComplete="new-password" value={confirm} onChange={(event) => setConfirm(event.target.value)} />
          </div>
        </>
      ) : (
        <p role="status" className="rounded-md bg-muted/60 px-3 py-2 text-sm">
          {t(($) => $.export.done, { name: saved })}
        </p>
      )}
      {working ? <Loading label={t(($) => $.export.working)} /> : null}
      {failure === 'vault_locked' ? (
        <VaultLockedNotice need="export" onUnlocked={() => setFailure(null)} />
      ) : failure === null ? null : (
        <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <p>{failure === 'mismatch' ? t(($) => $.export.mismatch) : failure === 'weak' ? t(($) => $.errors.weak_export_password) : t(($) => $.errors[failure])}</p>
          <ErrorGuide failure={failure} className="mt-2" />
        </div>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {saved === null ? t(($) => $.export.cancel) : t(($) => $.export.close)}
        </Button>
        {saved === null && (
          <Button type="submit" disabled={working}>
            <FileLock2 />
            {t(($) => $.export.submit)}
          </Button>
        )}
      </DialogFooter>
    </form>
  )
}

/**
 * Exports every collection and site of a workspace, with their secrets, to a file encrypted with a password chosen here (ADR-0017). The
 * password goes to the server, which seals the file and forgets it; the browser only saves the file.
 */
export function ExportDialog({ open, ...props }: ExportDialogProps) {
  return (
    <Dialog open={open} onOpenChange={() => props.onClose()}>
      <DialogContent className="sm:max-w-[480px]" showCloseButton={false}>
        {open && <ExportForm {...props} />}
      </DialogContent>
    </Dialog>
  )
}
