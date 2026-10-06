import { CircleCheck, LoaderCircle, PlugZap } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import type { SiteInput } from '@/lib/api'
import { useApi } from '@/lib/api/use-api'
import { ErrorGuide } from '@/features/errors/ErrorGuide'

import { unknownHostKeyOf, type UnknownHostKey } from '../quick/errors'
import { VaultDialog } from '../vault/VaultDialog'

import { CredentialDialog } from './CredentialDialog'

import { siteErrorKeyOf, type SiteErrorKey } from './errors'
import { endpointOf } from '../connectors/settings'

/** Where a connection test stands. */
type TestState =
  | { step: 'idle' }
  | { step: 'running' }
  | { step: 'ok'; entries: number; path: string }
  | { step: 'trust'; hostKey: UnknownHostKey; endpoint: { host: string; port: number } }
  | { step: 'unlock' }
  | { step: 'credential' }
  | { step: 'failed'; key: SiteErrorKey }

/** Props of {@link ConnectionTest}. */
export interface ConnectionTestProps {
  /** Workspace the site belongs to. */
  workspaceId: string
  /** The saved site the form edits, whose stored secrets fill the ones left blank; left out for a new site. */
  siteId?: string
  /** The site as the form holds it, checked first; undefined when the form does not pass its checks, which then show. */
  draft: () => Promise<SiteInput | undefined>
  /** How the site is named in the dialogs that ask for the master password or its password. */
  labels: { id: string; name: string; host: string; username: string }
}

/** Tries a site as the form holds it, saved or not, by listing the folder its server opens sessions in; the secrets left blank are the
 * stored ones. It asks to trust a new server key on the way, for the master password when the vault is locked, and for the site's password
 * when it asks for one when connecting. */
export function ConnectionTest({ workspaceId, siteId, draft, labels }: ConnectionTestProps) {
  const { t } = useTranslation('sites')
  const api = useApi()
  const [state, setState] = useState<TestState>({ step: 'idle' })

  const run = async (): Promise<void> => {
    const site = await draft()
    if (site === undefined) {
      setState({ step: 'idle' })
      return
    }
    setState({ step: 'running' })
    try {
      const result = await api.sites.test(workspaceId, site, siteId)
      setState({ step: 'ok', entries: result.entries, path: result.path })
    } catch (error) {
      const hostKey = unknownHostKeyOf(error)
      const key = siteErrorKeyOf(error)
      if (hostKey !== undefined) {
        setState({ step: 'trust', hostKey, endpoint: endpointOf(site) })
        return
      }
      setState(key === 'vault_locked' ? { step: 'unlock' } : key === 'credentials_required' && siteId !== undefined ? { step: 'credential' } : { step: 'failed', key })
    }
  }

  const trust = async (hostKey: UnknownHostKey, endpoint: { host: string; port: number }): Promise<void> => {
    try {
      await api.hostKeys.trust(workspaceId, { ...endpoint, ...hostKey })
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
            {t(($) => $.site.test.ok, { count: state.entries, path: state.path })}
          </p>
        )}
      </div>
      {state.step === 'failed' && (
        <div role="alert" className="text-sm text-destructive">
          <p>{t(($) => $.errors[state.key])}</p>
          <ErrorGuide failure={state.key} className="mt-2" />
        </div>
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
          <Button type="button" size="sm" className="self-start" onClick={() => trust(state.hostKey, state.endpoint)}>
            {t(($) => $.site.test.confirm)}
          </Button>
        </div>
      )}
      <VaultDialog
        mode={state.step === 'unlock' ? 'unlock' : null}
        target={labels}
        onClose={() => setState({ step: 'failed', key: 'vault_locked' })}
        onUnlocked={() => void run()}
      />
      <CredentialDialog
        workspaceId={workspaceId}
        target={state.step === 'credential' ? labels : null}
        onClose={() => setState({ step: 'failed', key: 'credentials_required' })}
        onGiven={() => void run()}
      />
    </div>
  )
}
