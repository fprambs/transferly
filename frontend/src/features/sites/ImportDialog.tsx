import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select } from '@/components/ui/select'
import { Loading } from '@/components/ui/loading'
import type { ImportOptions, ImportResult } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import { VaultLockedNotice } from '../vault/VaultLockedNotice'

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

/** The kinds of file the dialog imports. */
type ImportKind = 'filezilla' | 'transferly'

/** Tells a file exported by Transferly (ADR-0017) from a FileZilla site manager by its content, not its name. */
function importKindOf(text: string): ImportKind {
  try {
    const parsed: unknown = JSON.parse(text)
    return typeof parsed === 'object' && parsed !== null && (parsed as { format?: unknown }).format === 'transferly-export' ? 'transferly' : 'filezilla'
  } catch {
    return 'filezilla'
  }
}

/** The file chosen, read once: its kind and its text. */
interface Chosen {
  /** The file itself, sent as it is to the FileZilla import. */
  file: File
  /** What kind of file it is. */
  kind: ImportKind
  /** Its text, sent inside the Transferly import. */
  text: string
}

/** Imports sites from the site manager of FileZilla or from an encrypted export of Transferly: choose the file, look at what would happen,
 * and confirm. The kind of file is told by its content; an export also needs its password. */
export function ImportDialog({ open, workspaceId, onClose }: ImportDialogProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const queryClient = useQueryClient()
  const picker = useRef<HTMLInputElement>(null)
  const [chosen, setChosen] = useState<Chosen | null>(null)
  const [password, setPassword] = useState('')
  const [passwords, setPasswords] = useState(false)
  const [onConflict, setOnConflict] = useState<'skip' | 'rename'>('skip')
  const [result, setResult] = useState<ImportResult | null>(null)
  const [working, setWorking] = useState(false)
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)

  const run = async (dryRun: boolean): Promise<void> => {
    setWorking(true)
    setFailure(null)
    try {
      const { file, kind, text } = chosen as Chosen
      const options: ImportOptions = { dryRun, passwords, onConflict }
      setResult(
        kind === 'transferly'
          ? await api.imports.transferly(workspaceId, { password, file: text }, { dryRun, secrets: passwords, onConflict })
          : await api.imports.filezilla(workspaceId, file, options),
      )
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
            accept=".xml,.json,application/xml,text/xml,application/json"
            aria-label={t(($) => $.import.file)}
            onChange={(event) => {
              const file = (event.target.files as FileList)[0] as File | undefined
              setResult(null)
              setFailure(null)
              setChosen(null)
              if (file !== undefined) {
                void file.text().then((text) => setChosen({ file, kind: importKindOf(text), text }))
              }
            }}
          />
          {chosen === null ? null : <p className="text-xs text-muted-foreground">{t(($) => $.import.kind[chosen.kind])}</p>}
          {chosen?.kind === 'transferly' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="import-export-password">{t(($) => $.import.exportPassword)}</Label>
              <Input id="import-export-password" type="password" autoComplete="off" value={password} onChange={(event) => setPassword(event.target.value)} />
            </div>
          )}
          <label className="flex items-start gap-2">
            <input type="checkbox" className="mt-1" checked={passwords} onChange={(event) => setPasswords(event.target.checked)} />
            {chosen?.kind === 'transferly' ? (
              <span>
                {t(($) => $.import.secrets)}
                <span className="block text-xs text-muted-foreground">{t(($) => $.import.secretsHint)}</span>
              </span>
            ) : (
              <span>
                {t(($) => $.import.passwords)}
                <span className="block text-xs text-muted-foreground">{t(($) => $.import.passwordsHint)}</span>
              </span>
            )}
          </label>
          <label className="flex items-center gap-2">
            <span className="shrink-0">{t(($) => $.import.onConflict)}</span>
            <Select
              value={onConflict}
              options={[
                { value: 'skip', label: t(($) => $.import.skip) },
                { value: 'rename', label: t(($) => $.import.rename) },
              ]}
              onValueChange={(value) => setOnConflict(value as 'skip' | 'rename')}
            />
          </label>
          {working ? <Loading label={t(($) => $.import.working)} /> : null}
          {failure === 'vault_locked' ? (
            <VaultLockedNotice need="save" onUnlocked={() => setFailure(null)} />
          ) : failure === null ? null : (
            <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-destructive">
              <p>{t(($) => $.errors[failure])}</p>
              <ErrorGuide failure={failure} className="mt-2" />
            </div>
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
              <Button variant="outline" disabled={chosen === null || (chosen.kind === 'transferly' && password === '') || working} onClick={() => void run(true)}>
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
