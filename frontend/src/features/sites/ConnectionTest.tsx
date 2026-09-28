import { CircleCheck, LoaderCircle, PlugZap } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import type { Site } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'

import { unknownHostKeyOf, type UnknownHostKey } from '../quick/errors'
import { VaultDialog } from '../vault/VaultDialog'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'

/** Where a connection test stands. */
type TestState =
  | { step: 'idle' }
  | { step: 'running' }
  | { step: 'ok'; entries: number }
  | { step: 'trust'; hostKey: UnknownHostKey }
  | { step: 'unlock' }
  | { step: 'failed'; key: SiteErrorKey }

/** Props of {@link ConnectionTest}. */
export interface ConnectionTestProps {
  /** Workspace the site belongs to. */
  workspaceId: string
  /** The saved site to test. */
  site: Site
}

/** Tries a saved site by listing its root; it asks to trust a new server key on the way, and for the master password when the vault is locked. */
export function ConnectionTest({ workspaceId, site }: ConnectionTestProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const [state, setState] = useState<TestState>({ step: 'idle' })

  const run = async (): Promise<void> => {
    setState({ step: 'running' })
    try {
      const entries = await api.files.list(workspaceId, site.id, '/')
      setState({ step: 'ok', entries: entries.length })
    } catch (error) {
      const hostKey = unknownHostKeyOf(error)
      const key = siteErrorKeyOf(error)
      setState(hostKey !== undefined ? { step: 'trust', hostKey } : key === 'vault_locked' ? { step: 'unlock' } : { step: 'failed', key })
    }
  }

  const trust = async (hostKey: UnknownHostKey): Promise<void> => {
    try {
      await api.hostKeys.trust(workspaceId, { host: site.host, port: site.port, ...hostKey })
    } catch (error) {
      setState({ step: 'failed', key: siteErrorKeyOf(error) })
      return
    }
    await run()
  }

  return (
    <div className="flex flex-col gap-2 rounded-lg border p-3">
      <div className="flex items-center gap-3">
        <Button type="button" variant="outline" size="sm" onClick={run} disabled={state.step === 'running'}>
          {state.step === 'running' ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : <PlugZap />}
          {state.step === 'running' ? t(($) => $.site.test.running) : t(($) => $.site.test.run)}
        </Button>
        {state.step === 'ok' && (
          <p role="status" className="flex items-center gap-1.5 text-sm text-ok">
            <CircleCheck className="size-4" />
            {t(($) => $.site.test.ok, { count: state.entries })}
          </p>
        )}
      </div>
      {state.step === 'failed' && (
        <p role="alert" className="text-sm text-destructive">
          {t(($) => $.errors[state.key])}
        </p>
      )}
      {state.step === 'trust' && (
        <div role="alert" className="flex flex-col gap-2 text-sm">
          <p>{t(($) => $.site.test.trust)}</p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
            <dt className="text-muted-foreground">{t(($) => $.site.test.keyType)}</dt>
            <dd className="font-mono">{state.hostKey.keyType}</dd>
            <dt className="text-muted-foreground">{t(($) => $.site.test.fingerprint)}</dt>
            <dd className="font-mono break-all">{state.hostKey.fingerprint}</dd>
          </dl>
          <Button type="button" size="sm" className="self-start" onClick={() => trust(state.hostKey)}>
            {t(($) => $.site.test.confirm)}
          </Button>
        </div>
      )}
      <VaultDialog
        mode={state.step === 'unlock' ? 'unlock' : null}
        target={site}
        onClose={() => setState({ step: 'failed', key: 'vault_locked' })}
        onUnlocked={() => void run()}
      />
    </div>
  )
}
