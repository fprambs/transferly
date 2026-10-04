import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import type { Connector } from '@/lib/api'
import { mockAdmin } from '@/test/admin'
import { connector, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

/** Answers the catalog from a list the switches change, recording every change asked; failing makes the changes fail with code. */
function mockStore(connectors: Connector[], failing?: string) {
  const state = { connectors: [...connectors], changes: [] as Array<{ type: string; enabled: boolean }> }
  server.use(
    http.get('/api/v1/connectors', () => HttpResponse.json(state.connectors)),
    http.put('/api/v1/admin/connectors/:type', async ({ request, params }) => {
      if (failing !== undefined) {
        return problem(404, failing)
      }
      const { enabled } = (await request.json()) as { enabled: boolean }
      state.changes.push({ type: String(params.type), enabled })
      state.connectors = state.connectors.map((c) => (c.type === params.type ? { ...c, enabled } : c))
      return HttpResponse.json(state.connectors.find((c) => c.type === params.type))
    }),
  )
  return state
}

/** Opens the connectors tab of the administration. */
async function openStore() {
  await renderApp({ path: '/admin' })
  const user = userEvent.setup()
  await user.click(await screen.findByRole('tab', { name: 'Connectors' }))
  return user
}

/** The card of a connector in the store. */
async function card(name: string): Promise<HTMLElement> {
  return screen.findByRole('listitem', { name })
}

/**
 * Covers the store of connectors in the administration (ADR-0019).
 *
 * Use cases:
 * - every connector shows its icon, a plug when its icon is unknown, its maturity, whether it is offered and what it can do
 * - offering a connector needs no question; stopping to offer one asks first, can be canceled, and then tells the server
 * - a refused change explains why, and a catalog that could not load explains why and retries
 */
describe('connectors store', () => {
  it('shows the connectors with their maturity, state and capabilities', async () => {
    mockAdmin()
    mockStore([connector('sftp', 'SFTP'), { ...connector('s3', 'S3', false, 'beta'), icon: 'bucket' }])
    await openStore()

    const sftp = await card('SFTP')
    expect(sftp).toHaveTextContent('Stable')
    expect(sftp).toHaveTextContent('Offered')
    expect(within(sftp).getByRole('list', { name: 'What SFTP can do' })).toHaveTextContent('FoldersPermissionsModification timesResumeRename')
    expect(within(sftp).getByRole('switch', { name: 'Offer SFTP' })).toBeChecked()
    const s3 = await card('S3')
    expect(s3).toHaveTextContent('Beta')
    expect(s3).toHaveTextContent('Not offered')
    expect(within(s3).getByRole('switch', { name: 'Offer S3' })).not.toBeChecked()
  })

  it('offers a connector at once and asks before it stops offering one', async () => {
    mockAdmin()
    const state = mockStore([connector('sftp', 'SFTP'), connector('s3', 'S3', false, 'beta')])
    const user = await openStore()

    await user.click(within(await card('S3')).getByRole('switch', { name: 'Offer S3' }))
    await waitFor(() => expect(within(screen.getByRole('listitem', { name: 'S3' })).getByRole('switch')).toBeChecked())
    expect(state.changes).toEqual([{ type: 's3', enabled: true }])

    await user.click(within(await card('SFTP')).getByRole('switch', { name: 'Offer SFTP' }))
    const question = await screen.findByRole('dialog', { name: 'Stop offering SFTP?' })
    await user.click(within(question).getByRole('button', { name: 'Cancel' }))
    expect(state.changes).toHaveLength(1)

    await user.click(within(await card('SFTP')).getByRole('switch', { name: 'Offer SFTP' }))
    await user.click(within(await screen.findByRole('dialog', { name: 'Stop offering SFTP?' })).getByRole('button', { name: 'Stop offering' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Stop offering SFTP?' })).not.toBeInTheDocument())
    expect(state.changes).toEqual([
      { type: 's3', enabled: true },
      { type: 'sftp', enabled: false },
    ])
    expect(await card('SFTP')).toHaveTextContent('Not offered')
  })

  it('explains a refused change', async () => {
    mockAdmin()
    mockStore([connector('s3', 'S3', false)], 'connector_not_found')
    const user = await openStore()

    await user.click(within(await card('S3')).getByRole('switch', { name: 'Offer S3' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('This connector is not part of Transferly.')
  })

  it('explains a catalog that could not load and retries', async () => {
    mockAdmin()
    let fail = true
    let answer = () => undefined as void
    const held = new Promise<void>((resolve) => (answer = resolve))
    server.use(
      http.get('/api/v1/connectors', async () => {
        await held
        return fail ? problem(503, 'network_error') : HttpResponse.json([connector('sftp', 'SFTP')])
      }),
    )
    const user = await openStore()

    expect(await screen.findByText('Loading the connectors…')).toBeInTheDocument()
    answer()
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server.')
    fail = false
    await user.click(await screen.findByRole('button', { name: 'Retry' }))
    expect(await card('SFTP')).toBeInTheDocument()
  })
})
