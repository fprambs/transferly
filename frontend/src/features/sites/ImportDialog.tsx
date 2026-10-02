import { useQueryClient } from '@tanstack/react-query'
import { CircleAlert, CircleCheck, FileUp, LoaderCircle, Upload, X } from 'lucide-react'
import { useEffect, useRef, useState, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select } from '@/components/ui/select'
import { Loading } from '@/components/ui/loading'
import type { ImportOptions, ImportResult } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { cn } from '@/lib/utils'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import { VaultLockedNotice } from '../vault/VaultLockedNotice'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { filezillaMasterCheckDelayMs, filezillaMasterRejected, filezillaUsesCrypt } from './import-dialog-logic'
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

/** What the check of the FileZilla master password found. */
type MasterVerify = 'idle' | 'checking' | 'ok' | 'bad'

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
  const masterVerifyTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const [chosen, setChosen] = useState<Chosen | null>(null)
  const [fileName, setFileName] = useState('')
  const [dragging, setDragging] = useState(false)
  const [password, setPassword] = useState('')
  const [filezillaMaster, setFilezillaMaster] = useState('')
  const [passwords, setPasswords] = useState(false)
  const [onConflict, setOnConflict] = useState<'skip' | 'rename'>('skip')
  const [result, setResult] = useState<ImportResult | null>(null)
  const [working, setWorking] = useState(false)
  const [failure, setFailure] = useState<SiteErrorKey | null>(null)
  const [masterVerify, setMasterVerify] = useState<MasterVerify>('idle')

  const needsFilezillaMaster = chosen?.kind === 'filezilla' && filezillaUsesCrypt(chosen.text)
  const needsMasterForImport = needsFilezillaMaster === true && passwords

  useEffect(() => {
    return () => {
      if (masterVerifyTimer.current !== undefined) {
        clearTimeout(masterVerifyTimer.current)
      }
    }
  }, [])

  const clearMasterVerifyTimer = (): void => {
    if (masterVerifyTimer.current !== undefined) {
      clearTimeout(masterVerifyTimer.current)
      masterVerifyTimer.current = undefined
    }
  }

  const runMasterPasswordCheck = async (snapshot: {
    chosen: Chosen
    master: string
    importPasswords: boolean
    conflict: 'skip' | 'rename'
  }): Promise<void> => {
    const { chosen: file, master, importPasswords, conflict } = snapshot
    if (file.kind !== 'filezilla' || !filezillaUsesCrypt(file.text) || !importPasswords || master === '') {
      setMasterVerify('idle')
      return
    }
    try {
      const preview = await api.imports.filezilla(workspaceId, file.file, {
        dryRun: true,
        passwords: importPasswords,
        onConflict: conflict,
        filezillaMasterPassword: master,
      })
      setMasterVerify(filezillaMasterRejected(preview, importPasswords) ? 'bad' : 'ok')
    } catch {
      setMasterVerify('idle')
    }
  }

  const queueMasterPasswordCheck = (
    master: string,
    importPasswords: boolean,
    conflict: 'skip' | 'rename',
    file: Chosen | null,
  ): void => {
    clearMasterVerifyTimer()
    if (file === null || file.kind !== 'filezilla' || !filezillaUsesCrypt(file.text) || !importPasswords || master === '') {
      setMasterVerify('idle')
      return
    }
    setMasterVerify('checking')
    const snapshot = { chosen: file, master, importPasswords, conflict }
    masterVerifyTimer.current = setTimeout(() => {
      masterVerifyTimer.current = undefined
      void runMasterPasswordCheck(snapshot)
    }, filezillaMasterCheckDelayMs)
  }

  const pick = async (file: File | undefined): Promise<void> => {
    clearMasterVerifyTimer()
    setResult(null)
    setFailure(null)
    setMasterVerify('idle')
    setChosen(null)
    if (file === undefined) {
      setFileName('')
      return
    }
    setFileName(file.name)
    const text = await file.text()
    const kind = importKindOf(text)
    const next: Chosen = { file, kind, text }
    setChosen(next)
    const importPasswords = kind === 'filezilla' && filezillaUsesCrypt(text) ? true : passwords
    if (kind === 'filezilla' && filezillaUsesCrypt(text)) {
      setPasswords(true)
    }
    queueMasterPasswordCheck(filezillaMaster, importPasswords, onConflict, next)
  }

  const clearFile = (): void => {
    clearMasterVerifyTimer()
    setFileName('')
    setChosen(null)
    setResult(null)
    setMasterVerify('idle')
    ;(picker.current as HTMLInputElement).value = ''
  }

  const drop = (event: DragEvent<HTMLDivElement>): void => {
    event.preventDefault()
    setDragging(false)
    void pick(event.dataTransfer.files[0])
  }

  const buildOptions = (dryRun: boolean): ImportOptions => ({
    dryRun,
    passwords,
    onConflict,
    filezillaMasterPassword: chosen?.kind === 'filezilla' && filezillaMaster !== '' ? filezillaMaster : undefined,
  })

  const execute = async (dryRun: boolean, silent = false): Promise<ImportResult | null> => {
    if (chosen === null) {
      return null
    }
    if (!silent) {
      setWorking(true)
    }
    setFailure(null)
    try {
      const { file, kind, text } = chosen
      const next =
        kind === 'transferly'
          ? await api.imports.transferly(workspaceId, { password, file: text }, { dryRun, secrets: passwords, onConflict })
          : await api.imports.filezilla(workspaceId, file, buildOptions(dryRun))
      if (!silent) {
        setResult(next)
      }
      if (!dryRun) {
        await queryClient.invalidateQueries({ queryKey: ['workspaces', workspaceId] })
        await queryClient.invalidateQueries({ queryKey: sitesKeys.sites(workspaceId) })
        await queryClient.invalidateQueries({ queryKey: sitesKeys.collections(workspaceId) })
      }
      return next
    } catch (error) {
      if (!silent) {
        setFailure(siteErrorKeyOf(error))
      }
      return null
    } finally {
      if (!silent) {
        setWorking(false)
      }
    }
  }

  const preview = (): void => {
    void execute(true)
  }

  const confirmImport = async (): Promise<void> => {
    let previewResult = result?.dryRun === true ? result : null
    if (previewResult === null) {
      previewResult = await execute(true)
    }
    if (previewResult === null) {
      return
    }
    if (needsMasterForImport && filezillaMasterRejected(previewResult, passwords)) {
      setMasterVerify('bad')
      return
    }
    await execute(false)
  }

  const items = result?.items ?? []
  const counts = (kinds: readonly string[]): number => items.filter((item) => kinds.includes(item.status)).length
  const done = result !== null && result.dryRun === false
  const formReady =
    chosen !== null &&
    !working &&
    (chosen.kind !== 'transferly' || password !== '') &&
    (!needsMasterForImport || filezillaMaster !== '')
  const importBlocked = !formReady || (needsMasterForImport && masterVerify === 'bad')

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
            className="sr-only"
            tabIndex={-1}
            accept=".xml,.json,application/xml,text/xml,application/json"
            aria-label={t(($) => $.import.file)}
            onChange={(event) => void pick((event.target.files as FileList)[0])}
          />
          {chosen === null ? (
            <div
              onDragOver={(event) => {
                event.preventDefault()
                setDragging(true)
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={drop}
              className={cn(
                'flex flex-col items-center gap-2 rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground transition-colors',
                dragging && 'border-accent bg-accent/10',
              )}
            >
              <Button type="button" variant="outline" size="sm" aria-label={t(($) => $.import.fileChoose)} onClick={() => picker.current?.click()}>
                <Upload aria-hidden="true" />
                {t(($) => $.import.fileChoose)}
              </Button>
              <span>{t(($) => $.import.fileDrop)}</span>
            </div>
          ) : (
            <div className="flex items-start gap-3 rounded-md border bg-muted/30 px-3 py-2 text-xs" role="group" aria-label={t(($) => $.import.fileChosen)}>
              <FileUp aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-accent-text" />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <p className="truncate font-mono text-sm text-foreground">{fileName}</p>
                <p className="text-muted-foreground">{t(($) => $.import.kind[chosen.kind])}</p>
              </div>
              <Button type="button" variant="ghost" size="icon-sm" aria-label={t(($) => $.import.fileRemove)} onClick={clearFile}>
                <X aria-hidden="true" />
              </Button>
            </div>
          )}
          <p className="text-xs leading-relaxed text-muted-foreground">{t(($) => $.import.fileHint)}</p>
          {chosen?.kind === 'transferly' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="import-export-password">{t(($) => $.import.exportPassword)}</Label>
              <Input id="import-export-password" type="password" autoComplete="off" value={password} onChange={(event) => setPassword(event.target.value)} />
            </div>
          )}
          {needsFilezillaMaster ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="import-filezilla-master">{t(($) => $.import.filezillaMasterPassword)}</Label>
              <Input
                id="import-filezilla-master"
                type="password"
                autoComplete="off"
                value={filezillaMaster}
                onChange={(event) => {
                  const master = event.target.value
                  setFilezillaMaster(master)
                  queueMasterPasswordCheck(master, passwords, onConflict, chosen)
                }}
              />
              <p className="text-xs text-muted-foreground">{t(($) => $.import.filezillaMasterPasswordHint)}</p>
              {passwords && filezillaMaster !== '' ? <MasterPasswordStatus verify={masterVerify} /> : null}
            </div>
          ) : null}
          <label className="flex items-start gap-2">
            <input
              type="checkbox"
              className="mt-1"
              checked={passwords}
              onChange={(event) => {
                const importPasswords = event.target.checked
                setPasswords(importPasswords)
                setResult(null)
                queueMasterPasswordCheck(filezillaMaster, importPasswords, onConflict, chosen)
              }}
            />
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
              onValueChange={(value) => {
                const conflict = value as 'skip' | 'rename'
                setOnConflict(conflict)
                setResult(null)
                queueMasterPasswordCheck(filezillaMaster, passwords, conflict, chosen)
              }}
            />
          </label>
          {working ? <Loading label={t(($) => $.import.working)} /> : null}
          {failure === 'vault_locked' ? (
            <VaultLockedNotice need="save" onUnlocked={() => setFailure(null)} />
          ) : failure === null ? null : (
            <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-danger-text">
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
        <DialogFooter className="gap-2 sm:justify-end">
          <Button variant="outline" onClick={onClose}>
            {done ? t(($) => $.import.close) : t(($) => $.import.cancel)}
          </Button>
          {done ? null : (
            <>
              <Button variant="outline" disabled={!formReady} onClick={preview}>
                {t(($) => $.import.preview)}
              </Button>
              <Button disabled={importBlocked} onClick={() => void confirmImport()}>
                {t(($) => $.import.importNow)}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Props of {@link MasterPasswordStatus}. */
interface MasterPasswordStatusProps {
  /** What the last check found. */
  verify: MasterVerify
}

/** Feedback under the FileZilla master password field. */
function MasterPasswordStatus({ verify }: MasterPasswordStatusProps) {
  const { t } = useTranslation('sites')
  if (verify === 'idle') {
    return null
  }
  if (verify === 'checking') {
    return (
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground" role="status">
        <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
        {t(($) => $.import.filezillaMasterPasswordChecking)}
      </p>
    )
  }
  const ok = verify === 'ok'
  const Icon = ok ? CircleCheck : CircleAlert
  return (
    <p role={ok ? 'status' : 'alert'} className={cn('flex items-center gap-1.5 text-xs', ok ? 'text-ok' : 'text-destructive')}>
      <Icon aria-hidden="true" className="size-3.5 shrink-0" />
      {ok ? t(($) => $.import.filezillaMasterPasswordOk) : t(($) => $.import.filezillaMasterPasswordWrong)}
    </p>
  )
}
