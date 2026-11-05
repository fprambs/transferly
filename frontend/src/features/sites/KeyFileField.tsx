import { CircleAlert, CircleCheck, FileKey, LoaderCircle, Upload, X } from 'lucide-react'
import { useEffect, useRef, useState, type DragEvent } from 'react'
import { useController, type Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { SshKeyInfo } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import type { ApiError } from '@/lib/transport'
import { cn } from '@/lib/utils'
import type { SiteValues } from './site-form'

/** The largest key file read, as the server bounds it. */
export const maxKeyFileBytes = 64 * 1024

/** How long typing in the passphrase pauses before the key is checked again, in milliseconds. */
export const inspectDelayMs = 300

/** Why a key cannot be used, as the server names it for an unsupported key. */
type UnsupportedReason = 'key_type' | 'ppk_version' | 'encryption' | 'argon2d' | 'cost'

/** What is known of the chosen key file. */
type Inspection =
  | { state: 'checking' }
  | { state: 'read'; info: SshKeyInfo }
  | { state: 'failed'; failure: 'private_key_unreadable' | 'secret_too_large' | 'unknown' }
  | { state: 'unsupported'; reason: UnsupportedReason }

/** Props of {@link KeyFileField}. */
interface KeyFileFieldProps {
  /** The form the key belongs to. */
  control: Control<SiteValues>
  /** The secret of the key in the form, such as the site's or the jump host's. */
  name: `secrets.${string}`
  /** The secret of its passphrase. */
  passphraseName: `secrets.${string}`
  /** Identifier of the file button, which the row's label points at. */
  id: string
  /** Label of the passphrase input. */
  passphraseLabel: string
  /** Whether the site already has this key saved, which is kept unless another file is chosen. */
  saved: boolean
}

/** Turns a failed inspection into what the field says about it; the transport fails with an {@link ApiError} only. */
function inspectionOf(error: ApiError): Inspection {
  if (error.code === 'private_key_unsupported') {
    return { state: 'unsupported', reason: error.problem.params?.reason as UnsupportedReason }
  }
  return { state: 'failed', failure: error.code === 'private_key_unreadable' || error.code === 'secret_too_large' ? error.code : 'unknown' }
}

/**
 * A private key given as a file, chosen or dropped, as FileZilla takes it (4.8g). The file is read in the browser and the
 * server says what it holds by its content: the format, type, size and fingerprint, whether it is a public key by
 * mistake, and whether the passphrase opens it, checked again as the passphrase is typed. Nothing is stored until the
 * site is saved.
 */
export function KeyFileField({ control, name, passphraseName, id, passphraseLabel, saved }: KeyFileFieldProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const key = useController({ control, name })
  const passphrase = useController({ control, name: passphraseName })
  const picker = useRef<HTMLInputElement>(null)
  const [fileName, setFileName] = useState('')
  const [dragging, setDragging] = useState(false)
  const [tooLarge, setTooLarge] = useState(false)
  const [inspection, setInspection] = useState<Inspection>({ state: 'checking' })
  const [encrypted, setEncrypted] = useState(false)
  const keyText = (key.field.value as string | undefined) ?? ''
  const phrase = (passphrase.field.value as string | undefined) ?? ''

  useEffect(() => {
    if (keyText === '') {
      return
    }
    let current = true
    const timer = setTimeout(() => {
      setInspection({ state: 'checking' })
      api.sshKeys.inspect(keyText, phrase === '' ? undefined : phrase).then(
        (info) => {
          if (current) {
            setInspection({ state: 'read', info })
            setEncrypted(info.encrypted === true)
          }
        },
        (error: unknown) => current && setInspection(inspectionOf(error as ApiError)),
      )
    }, inspectDelayMs)
    return () => {
      current = false
      clearTimeout(timer)
    }
  }, [api, keyText, phrase])

  const choose = async (file: File | undefined): Promise<void> => {
    if (file === undefined) {
      return
    }
    setFileName(file.name)
    setInspection({ state: 'checking' })
    setEncrypted(false)
    passphrase.field.onChange('')
    setTooLarge(file.size > maxKeyFileBytes)
    key.field.onChange(file.size > maxKeyFileBytes ? '' : await file.text())
  }

  const clear = (): void => {
    setFileName('')
    setTooLarge(false)
    setEncrypted(false)
    key.field.onChange('')
    passphrase.field.onChange('')
    ;(picker.current as HTMLInputElement).value = ''
  }

  const drop = (event: DragEvent<HTMLDivElement>): void => {
    event.preventDefault()
    setDragging(false)
    void choose(event.dataTransfer.files[0])
  }

  const chosen = keyText !== '' || tooLarge
  const asksPassphrase = keyText !== '' && encrypted

  return (
    <div className="flex flex-col gap-2">
      <input
        ref={picker}
        type="file"
        className="sr-only"
        tabIndex={-1}
        aria-label={t(($) => $.site.keyFile.input)}
        onChange={(event) => void choose(event.target.files?.[0])}
      />
      {chosen ? (
        <div className="flex items-start gap-3 rounded-md border bg-muted/30 px-3 py-2 text-xs" role="group" aria-label={t(($) => $.site.keyFile.chosen)}>
          <FileKey aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-accent-text" />
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <p className="truncate font-mono text-sm text-foreground">{fileName}</p>
            {tooLarge ? (
              <Status tone="error" text={t(($) => $.site.keyFile.failures.secret_too_large)} />
            ) : (
              <Described inspection={inspection} />
            )}
          </div>
          <Button type="button" variant="ghost" size="icon-sm" aria-label={t(($) => $.site.keyFile.remove)} onClick={clear}>
            <X aria-hidden="true" />
          </Button>
        </div>
      ) : (
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
          <Button id={id} type="button" variant="outline" size="sm" aria-label={t(($) => $.site.keyFile.choose)} onClick={() => picker.current?.click()}>
            <Upload aria-hidden="true" />
            {t(($) => $.site.keyFile.choose)}
          </Button>
          <span>{saved ? t(($) => $.site.keyFile.saved) : t(($) => $.site.keyFile.drop)}</span>
        </div>
      )}
      <p className="text-xs leading-relaxed text-muted-foreground">{t(($) => $.site.keyFile.hint)}</p>
      {asksPassphrase && (
        <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3">
          <Label htmlFor={`${id}-passphrase`} className="font-normal text-muted-foreground">
            {passphraseLabel}
          </Label>
          <Input id={`${id}-passphrase`} type="password" autoComplete="off" autoFocus {...passphrase.field} />
        </div>
      )}
    </div>
  )
}

/** Props of {@link Described}. */
interface DescribedProps {
  /** What is known of the key. */
  inspection: Inspection
}

/** What the server read in the key file, and what the user must do next. */
function Described({ inspection }: DescribedProps) {
  const { t } = useTranslation('sites')

  switch (inspection.state) {
    case 'checking':
      return (
        <p className="flex items-center gap-1.5 text-muted-foreground" role="status">
          <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
          {t(($) => $.site.keyFile.checking)}
        </p>
      )
    case 'failed':
      return <Status tone="error" text={t(($) => $.site.keyFile.failures[inspection.failure])} />
    case 'unsupported':
      return <Status tone="error" text={t(($) => $.site.keyFile.reasons[inspection.reason])} />
  }
  const { info } = inspection
  const kind = [
    t(($) => $.site.keyFile.formats[info.format]),
    info.private ? t(($) => $.site.keyFile.private) : t(($) => $.site.keyFile.public),
    info.algorithm === undefined ? undefined : `${info.algorithm}${info.bits === undefined ? '' : ` · ${info.bits} bits`}`,
    info.encrypted ? t(($) => $.site.keyFile.encrypted) : undefined,
  ].filter((part) => part !== undefined)
  return (
    <>
      <p className="text-muted-foreground">{kind.join(' · ')}</p>
      {info.fingerprint !== undefined && <p className="font-mono break-all text-muted-foreground">{info.fingerprint}</p>}
      <Status tone={info.status === 'usable' ? 'ok' : info.status === 'passphrase_required' ? 'info' : 'error'} text={t(($) => $.site.keyFile.status[info.status])} />
    </>
  )
}

/** Props of {@link Status}. */
interface StatusProps {
  /** How the message reads: fine, a step to take, or a problem. */
  tone: 'ok' | 'info' | 'error'
  /** The message. */
  text: string
}

/** One line saying whether the key can be used. */
function Status({ tone, text }: StatusProps) {
  const Icon = tone === 'ok' ? CircleCheck : CircleAlert
  return (
    <p role={tone === 'error' ? 'alert' : 'status'} className={cn('flex items-start gap-1.5', tone === 'ok' ? 'text-ok' : tone === 'info' ? 'text-foreground' : 'text-destructive')}>
      <Icon aria-hidden="true" className="mt-px size-3.5 shrink-0" />
      {text}
    </p>
  )
}
