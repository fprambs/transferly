import { useQueryClient } from '@tanstack/react-query'
import { Ban, Plus, Server, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTable, Td, Th, Tr } from '@/components/page/DataTable'
import { Badge, Section } from '@/components/page/Page'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Loading } from '@/components/ui/loading'
import { RetryFailure } from '@/components/ui/retry-failure'
import type { Networks } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { AdminFailure } from './AdminMessage'
import { adminErrorKeyOf, type AdminErrorKey } from './errors'
import { adminKeys, useNetworks } from './use-admin'

/** Props of {@link NetworksForm}. */
interface NetworksFormProps {
  /** The networks in force. */
  networks: Networks
}

/**
 * The private networks as a table: the ones set at startup, which only a restart changes, and the ones administrators added, each with a
 * button to remove it; a field adds another, and saving sends the list to the server, which checks and writes each entry in its own form.
 */
function NetworksForm({ networks }: NetworksFormProps) {
  const { t } = useTranslation('admin')
  const api = useApi()
  const queryClient = useQueryClient()
  const [custom, setCustom] = useState<string[]>(networks.custom)
  const [draft, setDraft] = useState('')
  const [failure, setFailure] = useState<AdminErrorKey | null>(null)
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)

  const add = (): void => {
    const entry = draft.trim()
    if (entry !== '' && !custom.includes(entry)) {
      setCustom([...custom, entry])
    }
    setDraft('')
    setSaved(false)
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setSaved(false)
    setFailure(null)
    try {
      const now = await api.admin.setNetworks(custom)
      queryClient.setQueryData(adminKeys.networks, now)
      setCustom(now.custom)
      setSaved(true)
    } catch (error) {
      setFailure(adminErrorKeyOf(error))
    }
    setBusy(false)
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <Section
        id="networks-title"
        title={t(($) => $.networks.listTitle)}
        description={t(($) => $.networks.intro)}
        flush
        footer={
          <>
            <Button type="submit" disabled={busy}>
              {t(($) => $.networks.save)}
            </Button>
            {saved && (
              <p role="status" className="text-sm text-ok">
                {t(($) => $.networks.saved)}
              </p>
            )}
          </>
        }
      >
        <DataTable aria-labelledby="networks-title">
          <thead>
            <tr>
              <Th>{t(($) => $.networks.table.network)}</Th>
              <Th>{t(($) => $.networks.table.origin)}</Th>
              <Th className="w-12">
                <span className="sr-only">{t(($) => $.networks.table.actions)}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {networks.fixed.length === 0 ? (
              <Tr>
                <Td className="text-muted-foreground">{t(($) => $.networks.none)}</Td>
                <Td>
                  <Badge tone="info">{t(($) => $.networks.origin.fixed)}</Badge>
                </Td>
                <Td />
              </Tr>
            ) : (
              networks.fixed.map((network) => (
                <Tr key={`fixed-${network}`}>
                  <Td className="font-mono text-xs">{network}</Td>
                  <Td>
                    <Badge tone="info">{t(($) => $.networks.origin.fixed)}</Badge>
                  </Td>
                  <Td />
                </Tr>
              ))
            )}
            {custom.length === 0 ? (
              <Tr>
                <Td colSpan={3} className="text-muted-foreground">
                  {t(($) => $.networks.noneAdded)}
                </Td>
              </Tr>
            ) : (
              custom.map((network) => (
                <Tr key={`custom-${network}`}>
                  <Td className="font-mono text-xs">{network}</Td>
                  <Td>
                    <Badge tone="accent">{t(($) => $.networks.origin.custom)}</Badge>
                  </Td>
                  <Td className="text-end">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={t(($) => $.networks.remove, { network })}
                      onClick={() => {
                        setCustom(custom.filter((entry) => entry !== network))
                        setSaved(false)
                      }}
                    >
                      <X />
                    </Button>
                  </Td>
                </Tr>
              ))
            )}
          </tbody>
        </DataTable>
        <div className="flex flex-col gap-1.5 border-t px-5 py-4">
          <Label htmlFor="network-to-add">{t(($) => $.networks.addLabel)}</Label>
          <div className="flex max-w-md gap-2">
            <Input
              id="network-to-add"
              className="font-mono"
              spellCheck={false}
              autoComplete="off"
              placeholder={t(($) => $.networks.placeholder)}
              value={draft}
              aria-describedby="network-to-add-help"
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  add()
                }
              }}
            />
            <Button type="button" variant="outline" onClick={add}>
              <Plus />
              {t(($) => $.networks.add)}
            </Button>
          </div>
          <p id="network-to-add-help" className="text-xs leading-relaxed text-muted-foreground">
            {t(($) => $.networks.help)}
          </p>
        </div>
      </Section>
      <AdminFailure failure={failure} />
    </form>
  )
}

/** The private networks sites may reach, beside the ranges that are never allowed. */
export function NetworksTab() {
  const { t } = useTranslation('admin')
  const networks = useNetworks()

  if (networks.loading) {
    return <Loading label={t(($) => $.networks.loading)} />
  }
  if (networks.data === undefined) {
    return <RetryFailure message={t(($) => $.errors[adminErrorKeyOf(networks.error)])} retryLabel={t(($) => $.retry)} onRetry={networks.refetch} />
  }
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
      <NetworksForm networks={networks.data} />
      <aside className="flex flex-col gap-4">
        <section className="flex flex-col gap-2 rounded-xl border border-info/30 bg-info/8 px-5 py-4 text-sm leading-relaxed">
          <h2 className="flex items-center gap-2 font-semibold text-info">
            <Server aria-hidden="true" className="size-4" />
            {t(($) => $.networks.fixed)}
          </h2>
          <p>{t(($) => $.networks.fixedNote)}</p>
        </section>
        <section className="flex flex-col gap-2 rounded-xl border border-warn/30 bg-warn/8 px-5 py-4 text-sm leading-relaxed">
          <h2 className="flex items-center gap-2 font-semibold text-warn">
            <Ban aria-hidden="true" className="size-4" />
            {t(($) => $.networks.neverTitle)}
          </h2>
          <p>{t(($) => $.networks.never)}</p>
        </section>
      </aside>
    </div>
  )
}
