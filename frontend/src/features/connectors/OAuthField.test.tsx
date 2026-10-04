import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { useForm, useWatch } from 'react-hook-form'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Connector } from '@/lib/api'
import { builtinConnectors, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { oauthChannel, type OAuthOutcome } from './OAuthField'
import { SchemaFields, type SchemaValues } from './SchemaFields'

/** Google Drive with its real schema, whose account is signed in with Google. */
const drive = builtinConnectors.find((connector) => connector.type === 'gdrive') as Connector

/** A form with the fields of Google Drive, which shows its values as they change. */
function Harness({ saved = [], connector = drive }: { saved?: string[]; connector?: Connector }) {
  const { control, register } = useForm<SchemaValues>({ defaultValues: { config: {}, secrets: {} } })
  const values = useWatch({ control })
  return (
    <>
      <SchemaFields connector={connector} groups={['connection', 'access']} control={control} register={register} errors={{}} saved={saved} idPrefix="site" />
      <pre aria-label="values">{JSON.stringify(values)}</pre>
    </>
  )
}

/** Publishes an outcome of a sign-in, as the page of the callback does. */
async function publish(outcome: Partial<OAuthOutcome>): Promise<void> {
  const channel = new BroadcastChannel(oauthChannel)
  await act(async () => {
    channel.postMessage({ source: oauthChannel, ...outcome })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  channel.close()
}

/** A popup window that remembers where it was sent and whether it was closed. */
function popup() {
  const opened = { location: { href: '' }, close: vi.fn() }
  vi.spyOn(window, 'open').mockReturnValue(opened as unknown as Window)
  return opened
}

/** Answers the start of a sign-in with Google with the consent page and the state. */
function mockAuthorize(state = 'state-1') {
  server.use(http.post('/api/v1/oauth/google/authorizations', () => HttpResponse.json({ url: 'https://accounts.example/consent', state }, { status: 201 })))
}

afterEach(() => vi.restoreAllMocks())

/**
 * Covers {@link OAuthField}: an account signed in with Google (ADR-0024).
 *
 * Scenarios:
 * - connecting opens the consent page in a popup and waits; an outcome of another sign-in is ignored, and the outcome of this one fills the
 *   refresh token and the account, which then reads as connected
 * - a refused sign-in, and one that fails in a way the field does not know, are explained
 * - a blocked popup, an instance without the sign-in with Google, and a failure to start are explained, closing the popup
 * - waiting can be given up
 * - a saved account offers to use another one, and an OAuth field without an account field connects too
 */
describe('OAuthField', () => {
  it('connects an account and fills its token and email', async () => {
    const opened = popup()
    mockAuthorize()
    const user = userEvent.setup()
    await renderWithProviders(<Harness />)

    expect(screen.getByLabelText('Shared drive')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Waiting for Google')
    expect(opened.location.href).toBe('https://accounts.example/consent')
    expect(screen.getByRole('button', { name: 'Connect with Google' })).toBeDisabled()

    await publish({ state: 'another', refreshToken: 'stolen', email: 'mallory@example.com' })
    expect(screen.getByRole('status')).toBeInTheDocument()
    await publish({ state: 'state-1', refreshToken: 'refresh-1', email: 'tester@example.com' })
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
    expect(screen.getByText('tester@example.com')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Use another account' })).toBeEnabled()
    expect(screen.getByLabelText('values')).toHaveTextContent('"refresh_token":"refresh-1"')
    expect(screen.getByLabelText('values')).toHaveTextContent('"account_email":"tester@example.com"')
  })

  it('explains a sign-in that failed', async () => {
    popup()
    mockAuthorize()
    const user = userEvent.setup()
    await renderWithProviders(<Harness />)

    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    await screen.findByRole('status')
    await publish({ state: 'state-1', error: 'denied' })
    expect(await screen.findByRole('alert')).toHaveTextContent('Google did not grant access')

    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    await screen.findByRole('status')
    await publish({ state: 'state-1', error: 'gremlins' })
    expect(await screen.findByRole('alert')).toHaveTextContent('Signing in with Google failed')
  })

  it('explains a blocked popup and a sign-in that cannot start', async () => {
    const user = userEvent.setup()
    vi.spyOn(window, 'open').mockReturnValue(null)
    await renderWithProviders(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('blocked the window of Google')

    let opened = popup()
    server.use(http.post('/api/v1/oauth/google/authorizations', () => problem(409, 'oauth_not_configured')))
    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('not set up on this instance'))
    expect(opened.close).toHaveBeenCalled()

    opened = popup()
    server.use(http.post('/api/v1/oauth/google/authorizations', () => problem(500, 'internal_error')))
    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Signing in with Google failed'))
    expect(opened.close).toHaveBeenCalled()
  })

  it('gives up waiting', async () => {
    popup()
    mockAuthorize()
    const user = userEvent.setup()
    await renderWithProviders(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    await user.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Connect with Google' })).toBeEnabled()
  })

  it('offers another account for a saved one, and connects a field without an account field', async () => {
    popup()
    mockAuthorize()
    const user = userEvent.setup()
    const bare = { ...drive, fields: drive.fields.filter((field) => field.name !== 'account_email').map((field) => ({ ...field, account: undefined })) }
    await renderWithProviders(
      <>
        <Harness saved={['refresh_token']} />
        <Harness connector={bare} />
      </>,
    )
    expect(screen.getByRole('button', { name: 'Use another account' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Connect with Google' }))
    await screen.findByRole('status')
    await publish({ state: 'state-1', refreshToken: 'refresh-2' })
    await waitFor(() => expect(screen.getAllByLabelText('values')[1]).toHaveTextContent('"refresh_token":"refresh-2"'))
    expect(screen.getAllByLabelText('values')[1]).not.toHaveTextContent('account_email')
  })
})
