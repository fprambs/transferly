import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { NativeSelect } from '@/components/ui/native-select'
import type { ImportOptions, ImportResult } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { sitesKeys } from './use-sites-data'

/** Props of {@link ImportDialog}. */
export interface ImportDialogProps {
  /** Whether the dialog is open. */
  open: boolean
  /** Workspace that receives the sites. */
  workspaceId: string
  /** Called when the dialog is dismissed. */
  onClose: () => void
}

/** The reasons the interface has a text for; any other code is shown as it is. */
const knownReasons = [
  'unsupported_protocol',
  'implicit_ftps_port',
  'key_file_login',
  'missing_host',
  'name_taken',
  'invalid_name',
  'invalid_host',
  'invalid_port',
  'invalid_protocol',
  'invalid_username',
  'invalid_auth_method',
  'secret_too_large',
  'destination_blocked',
  'host_unresolvable',
  'collection_too_deep',
  'collection_name_taken',
] as const

/** Tells whether a code is one of the {@link knownReasons}. */
function isKnown(code: string): code is (typeof knownReasons)[number] {
  return (knownReasons as readonly string[]).includes(code)
}

/** Imports the site manager of FileZilla: choose the file, look at what would happen, and confirm. */
export function ImportDialog({ open, workspaceId, onClose }: ImportDialogProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const queryClient = useQueryClient()
  const picker = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [passwords, setPasswords] = useState(false)
  const [onConflict, setOnConflict] = useState<'skip' | 'rename'>('skip')
  const [result, setResult] = useState<ImportResult | null>(null)
  const [working, setWorking] = useState(false)
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)

  const run = async (dryRun: boolean): Promise<void> => {
    setWorking(true)
    setFailure(null)
    try {
      const options: ImportOptions = { dryRun, passwords, onConflict }
      setResult(await api.imports.filezilla(workspaceId, file as File, options))
      if (!dryRun) {
        await queryClient.invalidateQueries({ queryKey: ['workspaces', workspaceId] })
        await queryClient.invalidateQueries({ queryKey: sitesKeys.sites(workspaceId) })
        await queryClient.invalidateQueries({ queryKey: sitesKeys.collections(workspaceId) })
      }
    } catch (error) {
      setFailure(siteErrorKeyOf(error))
    } finally {
      setWorking(false)
    }
  }

  const items = result?.items ?? []
  const counts = (kinds: readonly string[]): number => items.filter((item) => kinds.includes(item.status)).length
  const done = result !== null && !result.dryRun

  return (
    <Dialog open={open} onOpenChange={() => onClose()}>
      <DialogContent className="sm:max-w-[560px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.import.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.import.description)}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3 text-sm">
          <input
            ref={picker}
            type="file"
            accept=".xml,application/xml,text/xml"
            aria-label={t(($) => $.import.file)}
            onChange={(event) => {
              setFile((event.target.files as FileList)[0] ?? null)
              setResult(null)
              setFailure(null)
            }}
          />
          <label className="flex items-start gap-2">
            <input type="checkbox" className="mt-1" checked={passwords} onChange={(event) => setPasswords(event.target.checked)} />
            <span>
              {t(($) => $.import.passwords)}
              <span className="block text-xs text-muted-foreground">{t(($) => $.import.passwordsHint)}</span>
            </span>
          </label>
          <label className="flex items-center gap-2">
            <span className="shrink-0">{t(($) => $.import.onConflict)}</span>
            <NativeSelect value={onConflict} onChange={(event) => setOnConflict(event.target.value as 'skip' | 'rename')}>
              <option value="skip">{t(($) => $.import.skip)}</option>
              <option value="rename">{t(($) => $.import.rename)}</option>
            </NativeSelect>
          </label>
          {working ? <p role="status">{t(($) => $.import.working)}</p> : null}
          {failure === null ? null : (
            <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-destructive">
              {t(($) => $.errors[failure])}
            </p>
          )}
          {result === null ? null : (
            <div>
              <p className="font-medium">
                {done
                  ? t(($) => $.import.summaryDone, { count: counts(['created']), skipped: counts(['skipped']), failed: counts(['failed']) })
                  : t(($) => $.import.summaryPreview, { count: counts(['would_create']), skipped: counts(['skipped', 'failed']) })}
              </p>
              <ul aria-label={t(($) => $.import.title)} className="mt-2 max-h-56 overflow-y-auto rounded-md border">
                {items.map((item, index) => (
                  <li key={`${item.path}/${item.name}-${index}`} className="flex flex-wrap items-baseline gap-x-2 border-b px-3 py-1.5 last:border-b-0">
                    <span className="font-medium">{item.name}</span>
                    {item.path === '' ? null : <span className="font-mono text-xs text-muted-foreground">{item.path}</span>}
                    <span className="text-xs">{t(($) => $.import.status[item.status])}</span>
                    {item.reason === undefined ? null : (
                      <span className="text-xs text-muted-foreground">{isKnown(item.reason) ? t(($) => $.import.reasons[item.reason as (typeof knownReasons)[number]]) : item.reason}</span>
                    )}
                    {item.passwordStored ? <span className="text-xs text-muted-foreground">{t(($) => $.import.password.stored)}</span> : null}
                    {item.passwordSkipped ? <span className="text-xs text-muted-foreground">{t(($) => $.import.password.skipped)}</span> : null}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {done ? t(($) => $.import.close) : t(($) => $.import.cancel)}
          </Button>
          {done ? null : (
            <>
              <Button variant="outline" disabled={file === null || working} onClick={() => void run(true)}>
                {t(($) => $.import.preview)}
              </Button>
              <Button disabled={result === null || !result.dryRun || working} onClick={() => void run(false)}>
                {t(($) => $.import.importNow)}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
