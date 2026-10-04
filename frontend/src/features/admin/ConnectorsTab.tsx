import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConnectorIcon } from '@/components/brand/ConnectorIcon'
import { Badge, Section } from '@/components/page/Page'
import { Loading } from '@/components/ui/loading'
import { RetryFailure } from '@/components/ui/retry-failure'
import { Switch } from '@/components/ui/switch'
import type { Connector } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { connectorsKey, useConnectors } from '../connectors/use-connectors'

import { AdminFailure } from './AdminMessage'
import { ConfirmDialog } from './ConfirmDialog'
import { adminErrorKeyOf, type AdminErrorKey } from './errors'

/** The capabilities a card lists, in this order, when the connector has them. */
const capabilityKeys = ['folders', 'permissions', 'setTimes', 'resume', 'rename', 'serverCopy'] as const

/** Props of {@link ConnectorCard}. */
interface ConnectorCardProps {
  /** The connector to draw. */
  connector: Connector
  /** Whether a change of this connector is being saved. */
  busy: boolean
  /** Asks to offer the connector or to stop offering it. */
  onChange: (enabled: boolean) => void
}

/** One connector of the store: its icon, name, maturity, what it can do, and a switch that offers it or stops offering it. */
function ConnectorCard({ connector, busy, onChange }: ConnectorCardProps) {
  const { t } = useTranslation('admin')
  const titleId = `connector-${connector.type}`

  return (
    <li aria-labelledby={titleId} className="flex flex-col gap-3 rounded-lg border bg-background p-4">
      <div className="flex items-center gap-3">
        <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-foreground">
          <ConnectorIcon icon={connector.icon} className="size-6" />
        </span>
        <h3 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold">
          {connector.name}
        </h3>
        <Switch
          aria-label={t(($) => $.connectors.offer, { name: connector.name })}
          checked={connector.enabled}
          disabled={busy}
          onCheckedChange={(checked) => onChange(checked)}
        />
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge tone={connector.maturity === 'stable' ? 'ok' : 'warn'}>{t(($) => $.connectors.maturity[connector.maturity])}</Badge>
        <Badge tone={connector.enabled ? 'accent' : 'muted'} dot>
          {connector.enabled ? t(($) => $.connectors.offered) : t(($) => $.connectors.notOffered)}
        </Badge>
      </div>
      <ul aria-label={t(($) => $.connectors.capabilities, { name: connector.name })} className="flex flex-wrap gap-1.5">
        {capabilityKeys
          .filter((key) => connector.capabilities[key])
          .map((key) => (
            <li key={key} className="rounded-md bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
              {t(($) => $.connectors.capability[key])}
            </li>
          ))}
      </ul>
    </li>
  )
}

/**
 * The store of the instance (ADR-0019): the connectors Transferly is built with, and a switch on each that decides whether sites can use
 * it. Stopping to offer one asks first, since its sites stop connecting.
 */
export function ConnectorsTab() {
  const { t } = useTranslation('admin')
  const api = useApi()
  const queryClient = useQueryClient()
  const connectors = useConnectors()
  const [failure, setFailure] = useState<AdminErrorKey | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [disabling, setDisabling] = useState<Connector | null>(null)

  const save = async (connector: Connector, enabled: boolean) => {
    await api.connectors.setEnabled(connector.type, enabled)
    await queryClient.invalidateQueries({ queryKey: connectorsKey })
  }
  const change = async (connector: Connector, enabled: boolean) => {
    if (!enabled) {
      setDisabling(connector)
      return
    }
    setFailure(null)
    setBusy(connector.type)
    try {
      await save(connector, true)
    } catch (error) {
      setFailure(adminErrorKeyOf(error))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <AdminFailure failure={failure} />
      <Section id="connectors-title" title={t(($) => $.connectors.title)} description={t(($) => $.connectors.description)}>
        {connectors.loading ? (
          <Loading label={t(($) => $.connectors.loading)} />
        ) : connectors.data === undefined ? (
          <RetryFailure message={t(($) => $.errors[adminErrorKeyOf(connectors.error)])} retryLabel={t(($) => $.retry)} onRetry={connectors.refetch} />
        ) : (
          <ul aria-labelledby="connectors-title" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {connectors.data.map((connector) => (
              <ConnectorCard key={connector.type} connector={connector} busy={busy === connector.type} onChange={(enabled) => void change(connector, enabled)} />
            ))}
          </ul>
        )}
      </Section>
      {disabling !== null && (
        <ConfirmDialog
          open
          title={t(($) => $.connectors.disable.title, { name: disabling.name })}
          body={t(($) => $.connectors.disable.body)}
          confirm={t(($) => $.connectors.disable.confirm)}
          onClose={() => setDisabling(null)}
          onConfirm={() => save(disabling, false)}
        />
      )}
    </div>
  )
}
