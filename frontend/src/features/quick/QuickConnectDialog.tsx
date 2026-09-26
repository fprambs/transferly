import { zodResolver } from '@hookform/resolvers/zod'
import { LoaderCircle, Lock, Zap } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { QuickConnectInput, QuickConnection } from '@/lib/api'
import { useApi, type Api } from '@/lib/api/use-api'

import { quickErrorKeyOf, unknownHostKeyOf, type QuickErrorKey, type UnknownHostKey } from './errors'
import { defaultPorts, quickSchema, toQuickInput, type QuickValues } from './schema'
import { TrustPanel } from './TrustPanel'

/** A connection attempt waiting for the user to trust the server key. */
interface PendingTrust {
  /** What was asked to connect to. */
  input: QuickConnectInput
  /** The key the server presented. */
  hostKey: UnknownHostKey
  /** Workspace the connection belongs to. */
  workspaceId: string
}

/** Reads the protocol of a tab, which is one of the three the tabs offer. */
function toProtocol(value: unknown): QuickConnectInput['protocol'] {
  return value === 'ftps' || value === 'ftp' ? value : 'sftp'
}

/** Runs a call and reports how long it took, in milliseconds. */
async function timed<T>(run: () => Promise<T>): Promise<{ value: T; elapsedMs: number }> {
  const started = performance.now()
  const value = await run()
  return { value, elapsedMs: Math.round(performance.now() - started) }
}

/** Opens a quick connection and proves it works by listing the root; a connection that fails is closed again. */
async function connect(api: Api, workspaceId: string, input: QuickConnectInput): Promise<QuickConnection> {
  const connection = await api.quick.create(workspaceId, input)
  try {
    await api.files.list(workspaceId, connection.id, '/')
  } catch (error) {
    await api.quick.close(workspaceId, connection.id).catch(() => undefined)
    throw error
  }
  return connection
}

/** Props of {@link QuickConnectDialog}. */
export interface QuickConnectDialogProps {
  /** Whether the dialog is open. */
  open: boolean
  /** Workspace the connection belongs to. */
  workspaceId: string | undefined
  /** Called when the dialog asks to open or close. */
  onOpenChange: (open: boolean) => void
  /** Called with the connection once it is open, how long it took in milliseconds and its workspace. */
  onConnected: (connection: QuickConnection, elapsedMs: number, workspaceId: string) => void
}

/** Form to try a server without saving it; it asks to trust unknown server keys on the way. */
export function QuickConnectDialog({ open, workspaceId, onOpenChange, onConnected }: QuickConnectDialogProps) {
  const { t } = useTranslation('quick')
  const api = useApi()
  const [pending, setPending] = useState<PendingTrust | null>(null)
  const [failure, setFailure] = useState<QuickErrorKey | null>(null)
  const [protocol, setProtocol] = useState<QuickConnectInput['protocol']>('sftp')
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<QuickValues>({ resolver: zodResolver(quickSchema), defaultValues: { host: '', port: '', username: '', password: '' } })

  const attempt = async (input: QuickConnectInput): Promise<void> => {
    if (workspaceId === undefined) {
      return
    }
    try {
      const { value, elapsedMs } = await timed(() => connect(api, workspaceId, input))
      setPending(null)
      reset()
      onOpenChange(false)
      onConnected(value, elapsedMs, workspaceId)
    } catch (error) {
      const hostKey = unknownHostKeyOf(error)
      setPending(hostKey === undefined ? null : { input, hostKey, workspaceId })
      setFailure(hostKey === undefined ? quickErrorKeyOf(error) : null)
    }
  }

  const submit = handleSubmit((values) => attempt(toQuickInput(protocol, values)))

  const trust = async ({ input, hostKey, workspaceId: owner }: PendingTrust): Promise<void> => {
    try {
      await api.hostKeys.trust(owner, { host: input.host, port: input.port as number, ...hostKey })
    } catch (error) {
      setPending(null)
      setFailure(quickErrorKeyOf(error))
      return
    }
    await attempt(input)
  }

  const close = (): void => {
    setPending(null)
    setFailure(null)
    reset()
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-[520px]">
        {pending === null ? (
          <form onSubmit={submit} noValidate className="flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <Zap className="size-5 text-warn" />
                {t(($) => $.title)}
              </DialogTitle>
              <DialogDescription>{t(($) => $.description)}</DialogDescription>
            </DialogHeader>
            <Tabs value={protocol} onValueChange={(value) => setProtocol(toProtocol(value))} aria-label={t(($) => $.protocol.label)}>
              <TabsList className="w-full">
                <TabsTrigger value="sftp">{t(($) => $.protocol.sftp)}</TabsTrigger>
                <TabsTrigger value="ftps">{t(($) => $.protocol.ftps)}</TabsTrigger>
                <TabsTrigger value="ftp">{t(($) => $.protocol.ftp)}</TabsTrigger>
              </TabsList>
            </Tabs>
            {protocol === 'ftp' ? (
              <p role="note" className="rounded-md bg-warn/10 px-3 py-2 text-xs">
                {t(($) => $.ftpWarning)}
              </p>
            ) : null}
            <div className="flex gap-3">
              <Field id="quick-host" label={t(($) => $.fields.host)} error={errors.host?.message} className="flex-1">
                <Input id="quick-host" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.host !== undefined} {...register('host')} />
              </Field>
              <Field id="quick-port" label={t(($) => $.fields.port)} error={errors.port?.message} className="w-24">
                <Input id="quick-port" className="font-mono" inputMode="numeric" placeholder={String(defaultPorts[protocol])} aria-invalid={errors.port !== undefined} {...register('port')} />
              </Field>
            </div>
            <div className="flex gap-3">
              <Field id="quick-username" label={t(($) => $.fields.username)} error={errors.username?.message} className="flex-1">
                <Input id="quick-username" className="font-mono" autoComplete="off" spellCheck={false} aria-invalid={errors.username !== undefined} {...register('username')} />
              </Field>
              <Field id="quick-password" label={t(($) => $.fields.password)} error={errors.password?.message} className="flex-1">
                <Input id="quick-password" type="password" autoComplete="off" aria-invalid={errors.password !== undefined} {...register('password')} />
              </Field>
            </div>
            <p className="flex items-center gap-2 text-xs text-muted-foreground">
              <Lock className="size-3.5" />
              {t(($) => $.memoryNote)}
            </p>
            {failure !== null && (
              <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {t(($) => $.errors[failure])}
              </p>
            )}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={close}>
                {t(($) => $.cancel)}
              </Button>
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting ? (
                  <>
                    <LoaderCircle className="animate-spin motion-reduce:animate-none" />
                    {t(($) => $.connecting)}
                  </>
                ) : (
                  t(($) => $.connect)
                )}
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <TrustPanel host={pending.input.host} hostKey={pending.hostKey} busy={isSubmitting} onBack={() => setPending(null)} onTrust={() => trust(pending)} />
        )}
      </DialogContent>
    </Dialog>
  )
}

/** Props of {@link Field}. */
interface FieldProps {
  /** Identifier of the input the label points at. */
  id: string
  /** Visible label. */
  label: string
  /** Translation key of the validation message, if any. */
  error: string | undefined
  /** Extra classes of the wrapper. */
  className: string
  /** The input. */
  children: ReactNode
}

/** Label, input and validation message of one form field. */
function Field({ id, label, error, className, children }: FieldProps) {
  const { t } = useTranslation('quick')

  return (
    <div className={`flex flex-col gap-1.5 ${className}`}>
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error !== undefined && (
        <p className="text-xs text-destructive">{t(($) => $.errors[error as 'hostRequired' | 'portInvalid' | 'usernameRequired' | 'passwordRequired'])}</p>
      )}
    </div>
  )
}
