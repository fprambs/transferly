import { LoaderCircle, Lock, Zap } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConnectorIcon } from '@/components/brand/ConnectorIcon'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select } from '@/components/ui/select'
import type { Connector, QuickConnectInput, QuickConnection } from '@/lib/api'
import { useApi, type Api } from '@/lib/api/use-api'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import { SchemaFields } from '../connectors/SchemaFields'
import { settle, startingConnector } from '../connectors/schema'
import { endpointOf } from '../connectors/settings'
import { useConnectorTexts } from '../connectors/use-connector-texts'
import { useConnectors } from '../connectors/use-connectors'

import { quickErrorKeyOf, unknownHostKeyOf, type QuickErrorKey, type UnknownHostKey } from './errors'
import { quickDefaults, quickResolver, toQuickInput, unattended, type QuickValues } from './schema'
import { TrustPanel } from './TrustPanel'

/** A connection attempt waiting for the user to trust the server key. */
interface PendingTrust {
  /** What was asked to connect to. */
  input: QuickConnectInput
  /** Where the server is, as its key is trusted. */
  endpoint: { host: string; port: number }
  /** The key the server presented. */
  hostKey: UnknownHostKey
  /** Workspace the connection belongs to. */
  workspaceId: string
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

/** Props of {@link QuickConnectForm}. */
interface QuickConnectFormProps extends Omit<QuickConnectDialogProps, 'open'> {
  /** The connectors the instance offers. */
  offered: readonly Connector[]
}

/** The form of a quick connection, drawn from the schema of the chosen connector, and the trust of an unknown server key on the way. */
function QuickConnectForm({ workspaceId, offered, onOpenChange, onConnected }: QuickConnectFormProps) {
  const { t } = useTranslation('quick')
  const texts = useConnectorTexts()
  const api = useApi()
  const [pending, setPending] = useState<PendingTrust | null>(null)
  const [failure, setFailure] = useState<QuickErrorKey | null>(null)
  const resolver = useMemo(() => quickResolver(offered), [offered])
  const starting = startingConnector(offered)
  const {
    register,
    handleSubmit,
    control,
    setValue,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<QuickValues>({ resolver, defaultValues: quickDefaults(starting) })
  const type = useWatch({ control, name: 'connector' })
  const connector = offered.find((candidate) => candidate.type === type) as Connector

  const attempt = async (input: QuickConnectInput, endpoint: PendingTrust['endpoint']): Promise<void> => {
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
      setPending(hostKey === undefined ? null : { input, endpoint, hostKey, workspaceId })
      setFailure(hostKey === undefined ? quickErrorKeyOf(error) : null)
    }
  }

  const submit = handleSubmit((values) => {
    return attempt(toQuickInput(values, connector), endpointOf({ connector: connector.type, config: settle(connector, values.config) }))
  })

  const trust = async ({ input, endpoint, hostKey, workspaceId: owner }: PendingTrust): Promise<void> => {
    try {
      await api.hostKeys.trust(owner, { ...endpoint, ...hostKey })
    } catch (error) {
      setPending(null)
      setFailure(quickErrorKeyOf(error))
      return
    }
    await attempt(input, endpoint)
  }

  const close = (): void => {
    setPending(null)
    setFailure(null)
    reset()
    onOpenChange(false)
  }

  if (pending !== null) {
    return <TrustPanel host={pending.endpoint.host} hostKey={pending.hostKey} busy={isSubmitting} onBack={() => setPending(null)} onTrust={() => trust(pending)} />
  }
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          <Zap className="size-5 text-warn" />
          {t(($) => $.title)}
        </DialogTitle>
        <DialogDescription>{t(($) => $.description)}</DialogDescription>
      </DialogHeader>
      <div className="flex items-center gap-3">
        <label htmlFor="quick-connector" className="shrink-0 text-sm font-medium">
          {t(($) => $.protocol.label)}
        </label>
        <span aria-hidden="true" className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted">
          <ConnectorIcon icon={connector.icon} className="size-5" />
        </span>
        <Select
          id="quick-connector"
          aria-label={t(($) => $.protocol.label)}
          value={type}
          onValueChange={(value) => setValue('connector', value)}
          options={offered.map((candidate) => ({ value: candidate.type, label: texts.connector(candidate) }))}
        />
      </div>
      <div className="flex flex-col gap-4">
        <SchemaFields connector={connector} groups={['connection', 'access']} control={control} register={register} errors={errors} exclude={unattended} idPrefix="quick" />
      </div>
      <p className="flex items-center gap-2 text-xs text-muted-foreground">
        <Lock className="size-3.5" />
        {t(($) => $.memoryNote)}
      </p>
      {failure !== null && (
        <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-danger-text">
          <p>{t(($) => $.errors[failure])}</p>
          <ErrorGuide failure={failure} className="mt-2" />
        </div>
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
  )
}

/** Dialog to try a server without saving it, with any connector the instance offers; it waits for the catalog of connectors. */
export function QuickConnectDialog({ open, ...form }: QuickConnectDialogProps) {
  const offered = useConnectors().data?.filter((connector) => connector.enabled) ?? []
  return (
    <Dialog open={open} onOpenChange={() => form.onOpenChange(false)}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-[600px]">{open && offered.length > 0 && <QuickConnectForm offered={offered} {...form} />}</DialogContent>
    </Dialog>
  )
}
