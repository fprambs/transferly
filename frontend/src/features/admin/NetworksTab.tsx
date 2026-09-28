import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Loading } from '@/components/ui/loading'
import { RetryFailure } from '@/components/ui/retry-failure'
import type { Networks } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { AdminFailure } from './AdminMessage'
import { adminErrorKeyOf, type AdminErrorKey } from './errors'
import { adminKeys, useNetworks } from './use-admin'

/** Reads the entries of the text box: one network or address per line, without blank lines. */
function entriesOf(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
}

/** Props of {@link NetworksForm}. */
interface NetworksFormProps {
  /** The networks in force. */
  networks: Networks
}

/** The networks the server was started with, which cannot be changed here, and the editable list of the ones administrators add. */
function NetworksForm({ networks }: NetworksFormProps) {
  const { t } = useTranslation('admin')
  const api = useApi()
  const queryClient = useQueryClient()
  const [text, setText] = useState(networks.custom.join('\n'))
  const [failure, setFailure] = useState<AdminErrorKey | null>(null)
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setSaved(false)
    setFailure(null)
    try {
      const now = await api.admin.setNetworks(entriesOf(text))
      queryClient.setQueryData(adminKeys.networks, now)
      setText(now.custom.join('\n'))
      setSaved(true)
    } catch (error) {
      setFailure(adminErrorKeyOf(error))
    }
    setBusy(false)
  }

  return (
    <form onSubmit={submit} noValidate className="flex max-w-2xl flex-col gap-4">
      <section aria-labelledby="fixed-networks" className="rounded-lg border bg-card px-4 py-3.5">
        <h2 id="fixed-networks" className="text-sm font-semibold">
          {t(($) => $.networks.fixed)}
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">{t(($) => $.networks.fixedNote)}</p>
        {networks.fixed.length === 0 ? (
          <p className="mt-2 text-sm text-muted-foreground">{t(($) => $.networks.none)}</p>
        ) : (
          <ul className="mt-2 flex flex-wrap gap-2 font-mono text-xs">
            {networks.fixed.map((network) => (
              <li key={network} className="rounded-md bg-muted px-2 py-1">
                {network}
              </li>
            ))}
          </ul>
        )}
      </section>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="custom-networks">{t(($) => $.networks.custom)}</Label>
        <textarea
          id="custom-networks"
          rows={6}
          spellCheck={false}
          autoComplete="off"
          value={text}
          placeholder={t(($) => $.networks.placeholder)}
          onChange={(event) => setText(event.target.value)}
          className="w-full rounded-lg border bg-transparent px-3 py-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          aria-describedby="custom-networks-help"
        />
        <p id="custom-networks-help" className="text-xs leading-relaxed text-muted-foreground">
          {t(($) => $.networks.help)}
        </p>
      </div>
      <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm leading-relaxed">{t(($) => $.networks.never)}</p>
      <AdminFailure failure={failure} />
      {saved && (
        <p role="status" className="rounded-md bg-emerald-500/10 px-3 py-2 text-sm text-emerald-800 dark:text-emerald-300">
          {t(($) => $.networks.saved)}
        </p>
      )}
      <div>
        <Button type="submit" disabled={busy}>
          {t(($) => $.networks.save)}
        </Button>
      </div>
    </form>
  )
}

/** The private networks saved sites may reach: what the server was started with and what administrators added. */
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
    <div className="flex flex-col gap-4">
      <p className="max-w-2xl text-sm leading-relaxed text-muted-foreground">{t(($) => $.networks.intro)}</p>
      <NetworksForm networks={networks.data} />
    </div>
  )
}
