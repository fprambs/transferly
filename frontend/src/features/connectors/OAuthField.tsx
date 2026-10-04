import { useEffect, useState } from 'react'
import { useController, type Control, type Path } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useApi } from '@/lib/api/use-api'
import { ApiError } from '@/lib/transport'

import type { ConnectorField } from './schema'
import type { SchemaValues } from './SchemaFields'

/** The channel the page Google sends the browser back to publishes the outcome of a sign-in on (ADR-0024). */
export const oauthChannel = 'transferly-oauth'

/** The outcome of a sign-in, as the page of the callback publishes it. */
export interface OAuthOutcome {
  /** Always transferly-oauth, which tells the messages of the sign-in from others on the channel. */
  source: string
  /** The state of the sign-in the outcome belongs to. */
  state: string
  /** The refresh token of the account, when the sign-in succeeded. */
  refreshToken?: string
  /** The email of the account, when the sign-in succeeded. */
  email?: string
  /** Why the sign-in failed: denied, expired, no_refresh_token, not_configured or failed. */
  error?: string
}

/** Why connecting an account failed, as the field words it. */
type Failure = 'denied' | 'expired' | 'no_refresh_token' | 'not_configured' | 'failed' | 'blocked'

/** The failures the field words; any other reads as failed. */
const failures: readonly Failure[] = ['denied', 'expired', 'no_refresh_token', 'not_configured', 'failed', 'blocked']

/** Props of {@link OAuthField}. */
export interface OAuthFieldProps<T extends SchemaValues> {
  /** Identifier of the button, which the row's label points at. */
  id: string
  /** The OAuth field. */
  field: ConnectorField
  /** The form the field belongs to. */
  control: Control<T>
  /** Whether the site already has an account connected, which is kept unless another one is. */
  saved: boolean
}

/**
 * An account signed in with OAuth, such as a Google account (ADR-0024): a button opens the consent page of the provider in a popup,
 * and the refresh token that comes back fills the secret of the field, and the email the field that keeps the account. The outcome
 * arrives on a broadcast channel, since the popup is cut from the page once it visits the provider, and only the outcome of the
 * sign-in this field started is taken.
 */
export function OAuthField<T extends SchemaValues>({ id, field, control, saved }: OAuthFieldProps<T>) {
  const { t } = useTranslation('connectors')
  const api = useApi()
  const token = useController({ control, name: `secrets.${field.name}` as Path<T> })
  const account = useController({ control, name: `config.${field.account ?? field.name}` as Path<T> })
  const [waiting, setWaiting] = useState<string | null>(null)
  const [failure, setFailure] = useState<Failure | null>(null)
  const email = field.account === undefined ? '' : String(account.field.value ?? '')
  const connected = String(token.field.value ?? '') !== '' || saved
  const action = connected ? t(($) => $.oauth.reconnect) : t(($) => $.oauth.connect)

  useEffect(() => {
    if (waiting === null) {
      return
    }
    const channel = new BroadcastChannel(oauthChannel)
    channel.onmessage = (event: MessageEvent<OAuthOutcome>) => {
      const outcome = event.data
      if (outcome.source !== oauthChannel || outcome.state !== waiting) {
        return
      }
      setWaiting(null)
      if (outcome.error !== undefined || outcome.refreshToken === undefined) {
        setFailure(failures.find((known) => known === outcome.error) ?? 'failed')
        return
      }
      token.field.onChange(outcome.refreshToken)
      if (field.account !== undefined) {
        account.field.onChange(outcome.email)
      }
    }
    return () => channel.close()
  }, [waiting, token.field, account.field, field.account])

  const connect = async () => {
    setFailure(null)
    const popup = window.open('', oauthChannel, 'popup,width=520,height=680')
    if (popup === null) {
      setFailure('blocked')
      return
    }
    try {
      const started = await api.oauth.authorizeGoogle()
      popup.location.href = started.url
      setWaiting(started.state)
    } catch (error) {
      popup.close()
      setFailure(error instanceof ApiError && error.code === 'oauth_not_configured' ? 'not_configured' : 'failed')
    }
  }

  return (
    <div className="flex flex-col gap-2">
      {connected && email !== '' && (
        <p className="text-sm">
          {t(($) => $.oauth.connected)} <span className="font-medium">{email}</span>
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <Button id={id} type="button" variant={connected ? 'outline' : 'default'} size="sm" disabled={waiting !== null} aria-label={action} onClick={() => void connect()}>
          {action}
        </Button>
        {waiting !== null && (
          <span role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
            {t(($) => $.oauth.waiting)}
            <Button type="button" variant="link" size="sm" className="h-auto p-0" onClick={() => setWaiting(null)}>
              {t(($) => $.oauth.cancel)}
            </Button>
          </span>
        )}
      </div>
      {failure !== null && (
        <p role="alert" className="text-xs text-destructive">
          {t(($) => $.oauth.failures[failure])}
        </p>
      )}
    </div>
  )
}
