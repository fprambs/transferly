import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { Explorer } from '../explorer/Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** A saved site that asks for its password when connecting. */
const asking: Site = {
  connector: 'sftp', config: { host: 'bastion.example.com', port: '22', username: 'deploy', auth: 'ask', list_timezone: 'UTC', charset: 'utf-8' },
  id: 's1',
  name: 'Bastion',
  speedLimitBps: 0,
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** The one file of the site. */
const readme: RemoteEntry = { name: 'readme.txt', path: '/readme.txt', type: 'file', size: 10, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'minute' }

/** What the mock server answers the listing with, and the bodies of the passwords it was given. */
function mockServer(answer: (given: number) => Response): Array<{ password: string; remember: boolean }> {
  const given: Array<{ password: string; remember: boolean }> = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', () => answer(given.length)),
    http.put('/api/v1/workspaces/:id/sites/s1/credential', async ({ request }) => {
      given.push((await request.json()) as { password: string; remember: boolean })
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return given
}

/** Renders the explorer of the site. */
async function renderExplorer() {
  const user = userEvent.setup()
  await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/" onNavigate={() => undefined} />)
  return user
}

/**
 * Covers {@link CredentialNotice} and {@link CredentialDialog} in the explorer, for a site that asks for its password when connecting.
 *
 * Scenarios:
 * - the dialog opens at once naming the site, refuses an empty password, sends the typed one with "remember" as chosen, and the folder lists
 * - dismissing the dialog leaves a card whose button opens it again
 * - a password the server refuses opens the dialog again and says so
 * - a failure to hand over the password is explained in the dialog
 */
describe('credential notice', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [asking] })
  })

  it('asks for the password at once and lists the folder once it is given', async () => {
    const given = mockServer((count) => (count === 0 ? problem(409, 'credentials_required') : HttpResponse.json([readme])))
    const user = await renderExplorer()

    const dialog = await screen.findByRole('dialog', { name: 'Enter the password' })
    expect(dialog).toHaveTextContent('Bastion')
    expect(dialog).toHaveTextContent('bastion.example.com')
    expect(dialog).toHaveTextContent('deploy')
    expect(within(dialog).getByLabelText('Password', { exact: true })).toHaveFocus()

    await user.click(within(dialog).getByRole('button', { name: 'Connect' }))
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Enter the password.')
    expect(given).toHaveLength(0)

    await user.type(within(dialog).getByLabelText('Password', { exact: true }), 'typed')
    await user.click(within(dialog).getByRole('checkbox'))
    await user.click(within(dialog).getByRole('button', { name: 'Connect' }))

    expect(await screen.findByRole('row', { name: /readme.txt/ })).toBeInTheDocument()
    expect(given).toEqual([{ password: 'typed', remember: false }])
  })

  it('leaves a card that opens the dialog again when it is dismissed', async () => {
    mockServer(() => problem(409, 'credentials_required'))
    const user = await renderExplorer()

    await user.click(within(await screen.findByRole('dialog', { name: 'Enter the password' })).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByText('This site asks for its password')).toBeInTheDocument()
    expect(screen.getByText('deploy@bastion.example.com')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Enter the password' }))
    expect(await screen.findByRole('dialog', { name: 'Enter the password' })).toBeInTheDocument()
  })

  it('asks again, saying so, when the server refuses the password', async () => {
    const given = mockServer((count) => (count === 0 ? problem(409, 'credentials_required') : count === 1 ? problem(502, 'remote_auth_failed') : HttpResponse.json([readme])))
    const user = await renderExplorer()

    const dialog = await screen.findByRole('dialog', { name: 'Enter the password' })
    await user.type(within(dialog).getByLabelText('Password', { exact: true }), 'wrong')
    await user.click(within(dialog).getByRole('button', { name: 'Connect' }))

    const again = await screen.findByRole('dialog', { name: 'Enter the password' })
    expect(await within(again).findByRole('alert')).toHaveTextContent('The server refused that password. Enter it again.')
    await user.type(within(again).getByLabelText('Password', { exact: true }), 'right')
    await user.click(within(again).getByRole('button', { name: 'Connect' }))

    expect(await screen.findByRole('row', { name: /readme.txt/ })).toBeInTheDocument()
    expect(given.map((body) => body.password)).toEqual(['wrong', 'right'])
  })

  it('explains a failure to hand over the password', async () => {
    mockServer(() => problem(409, 'credentials_required'))
    server.use(http.put('/api/v1/workspaces/:id/sites/s1/credential', () => problem(409, 'credentials_not_asked')))
    const user = await renderExplorer()

    const dialog = await screen.findByRole('dialog', { name: 'Enter the password' })
    await user.type(within(dialog).getByLabelText('Password', { exact: true }), 'typed')
    await user.click(within(dialog).getByRole('button', { name: 'Connect' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('This site no longer asks for a password; open it again.')
    expect(within(dialog).getByRole('button', { name: 'Connect' })).toBeEnabled()
  })
})
