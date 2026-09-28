import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import type { AuditEvent } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData } from '@/test/handlers'
import { server } from '@/test/msw'
import { defaultLimits, failAdmin, mockAdmin } from '@/test/admin'
import { renderApp } from '@/test/render-app'
import { renderWithProviders } from '@/test/render'

import { AdminPage } from './AdminPage'
import { adminActionKey } from './admin-actions'
import { adminErrorKeyOf } from './errors'

/** Builds an event of the trail of the instance. */
function event(id: number, action: string, target: string, extra: Partial<AuditEvent> = {}): AuditEvent {
  return { id, action, target, meta: {}, at: '2026-09-26T12:00:00Z', actor: 'admin@example.com', ip: '203.0.113.5', ...extra }
}

/** Opens the row menu of an account and returns the menu. */
async function openActions(email: string) {
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: `Actions for ${email}` }))
  return user
}

/**
 * Covers the administration page.
 *
 * Scenarios:
 * - the accounts show their role, state and second step, and the signed-in account has no actions
 * - the filter narrows the accounts by email and explains when nothing matches
 * - disabling asks first, can be canceled, tells the server and shows the new state; enabling needs no question
 * - making somebody an administrator and back tells the server, and a failed change explains why
 * - removing the second step asks first, is unavailable for accounts without one, and a failure inside the question is shown in it
 * - the networks show what the server started with, save the entries one per line without blank lines, and explain a refusal
 * - the limits show what is in force and what the server started with, save the values in the units of the server, refuse what is not a number
 *   and fill in the starting values on request
 * - the audit log words every action, narrows by group, pages, and explains an empty trail
 * - a tab whose data could not load explains why and retries, instead of staying on its loading state forever
 * - people who are not administrators get a message instead of the page, and the account menu offers the page only to administrators
 * - the translated messages, the action keys and the error keys are the ones the server uses
 */
describe('administration', () => {
  it('lists the accounts', async () => {
    mockAdmin()
    await renderApp({ path: '/admin' })

    const list = await screen.findByRole('list', { name: 'Accounts' })
    const rows = within(list).getAllByRole('listitem')
    expect(rows).toHaveLength(4)
    expect(rows[0]).toHaveTextContent('admin@example.com')
    expect(rows[0]).toHaveTextContent('You')
    expect(rows[0]).toHaveTextContent('Administrator')
    expect(rows[1]).toHaveTextContent('User')
    expect(rows[1]).toHaveTextContent('Active')
    expect(rows[1]).toHaveTextContent('On')
    expect(rows[2]).toHaveTextContent('Disabled')
    expect(rows[2]).toHaveTextContent('Off')
    expect(screen.queryByRole('button', { name: 'Actions for admin@example.com' })).not.toBeInTheDocument()
  })

  it('filters the accounts', async () => {
    mockAdmin()
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await screen.findByRole('list', { name: 'Accounts' })

    await user.type(screen.getByLabelText('Filter accounts'), 'AGENCY')
    expect(within(screen.getByRole('list', { name: 'Accounts' })).getAllByRole('listitem')).toHaveLength(1)
    await user.clear(screen.getByLabelText('Filter accounts'))
    await user.type(screen.getByLabelText('Filter accounts'), 'nobody')
    expect(screen.getByText('No account matches.')).toBeInTheDocument()
  })

  it('disables an account after asking', async () => {
    const mock = mockAdmin()
    await renderApp({ path: '/admin' })
    const user = await openActions('maria@acme.dev')

    await user.click(await screen.findByRole('menuitem', { name: 'Disable account' }))
    const dialog = await screen.findByRole('dialog', { name: 'Disable maria@acme.dev?' })
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(mock.calls).toEqual([])

    await user.click(screen.getByRole('button', { name: 'Actions for maria@acme.dev' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Disable account' }))
    await user.click(within(await screen.findByRole('dialog', { name: 'Disable maria@acme.dev?' })).getByRole('button', { name: 'Disable account' }))

    await waitFor(() => expect(mock.calls).toEqual([{ method: 'PATCH', path: '/api/v1/admin/users/u-maria', body: { disabled: true } }]))
    await waitFor(() => expect(within(screen.getByRole('list', { name: 'Accounts' })).getAllByRole('listitem')[1]).toHaveTextContent('Disabled'))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('enables an account without asking', async () => {
    const mock = mockAdmin()
    await renderApp({ path: '/admin' })
    const user = await openActions('jorge@acme.dev')

    await user.click(await screen.findByRole('menuitem', { name: 'Enable account' }))

    await waitFor(() => expect(mock.calls).toEqual([{ method: 'PATCH', path: '/api/v1/admin/users/u-jorge', body: { disabled: false } }]))
    await waitFor(() => expect(within(screen.getByRole('list', { name: 'Accounts' })).getAllByRole('listitem')[2]).toHaveTextContent('Active'))
  })

  it('changes roles both ways', async () => {
    const mock = mockAdmin()
    await renderApp({ path: '/admin' })
    let user = await openActions('maria@acme.dev')
    await user.click(await screen.findByRole('menuitem', { name: 'Make administrator' }))
    await waitFor(() => expect(within(screen.getByRole('list', { name: 'Accounts' })).getAllByRole('listitem')[1]).toHaveTextContent('Administrator'))

    user = await openActions('lucia@agency.io')
    await user.click(await screen.findByRole('menuitem', { name: 'Make regular user' }))
    await waitFor(() => expect(within(screen.getByRole('list', { name: 'Accounts' })).getAllByRole('listitem')[3]).toHaveTextContent('User'))
    expect(mock.calls.map((c) => c.body)).toEqual([{ role: 'admin' }, { role: 'user' }])
  })

  it('explains a failed change', async () => {
    mockAdmin()
    failAdmin('patch', '/users/:id', 409, 'last_admin')
    await renderApp({ path: '/admin' })
    const user = await openActions('lucia@agency.io')

    await user.click(await screen.findByRole('menuitem', { name: 'Make regular user' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('This is the last active administrator')
  })

  it('removes the second step after asking', async () => {
    const mock = mockAdmin()
    await renderApp({ path: '/admin' })
    let user = await openActions('jorge@acme.dev')
    expect(await screen.findByRole('menuitem', { name: 'Remove second step' })).toHaveAttribute('aria-disabled', 'true')
    await user.keyboard('{Escape}')

    user = await openActions('maria@acme.dev')
    await user.click(await screen.findByRole('menuitem', { name: 'Remove second step' }))
    const dialog = await screen.findByRole('dialog', { name: 'Remove the second step of maria@acme.dev?' })
    expect(dialog).toHaveTextContent('the action is recorded')
    await user.click(within(dialog).getByRole('button', { name: 'Remove second step' }))

    await waitFor(() => expect(mock.calls).toEqual([{ method: 'POST', path: '/api/v1/admin/users/u-maria/second-factor/reset', body: undefined }]))
    await waitFor(() => expect(within(screen.getByRole('list', { name: 'Accounts' })).getAllByRole('listitem')[1]).toHaveTextContent('Off'))
  })

  it('shows a failure inside the question and keeps it open', async () => {
    mockAdmin()
    failAdmin('post', '/users/:id/second-factor/reset', 404, 'account_not_found')
    await renderApp({ path: '/admin' })
    const user = await openActions('maria@acme.dev')
    await user.click(await screen.findByRole('menuitem', { name: 'Remove second step' }))
    const dialog = await screen.findByRole('dialog', { name: 'Remove the second step of maria@acme.dev?' })

    await user.click(within(dialog).getByRole('button', { name: 'Remove second step' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('This account no longer exists.')
    expect(within(dialog).getByRole('button', { name: 'Remove second step' })).toBeEnabled()
  })

  it('saves the networks', async () => {
    const mock = mockAdmin()
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: 'Networks' }))

    expect(await screen.findByText('172.16.0.0/12')).toBeInTheDocument()
    const box = screen.getByLabelText('Added by administrators')
    expect(box).toHaveValue('192.168.1.0/24')
    await user.clear(box)
    await user.type(box, '  10.20.0.5  {Enter}{Enter}192.168.5.0/24')
    await user.click(screen.getByRole('button', { name: 'Save networks' }))

    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')
    expect(mock.calls).toEqual([{ method: 'PUT', path: '/api/v1/admin/networks', body: { networks: ['10.20.0.5', '192.168.5.0/24'] } }])
    expect(screen.getByLabelText('Added by administrators')).toHaveValue('10.20.0.5/32\n192.168.5.0/24')
  })

  it('says when no network was set at startup, and explains a refusal', async () => {
    mockAdmin({ networks: { fixed: [], custom: [] } })
    failAdmin('put', '/networks', 422, 'invalid_network')
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: 'Networks' }))

    expect(await screen.findByText('None.')).toBeInTheDocument()
    await user.type(screen.getByLabelText('Added by administrators'), '0.0.0.0/0')
    await user.click(screen.getByRole('button', { name: 'Save networks' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('not a network that can be allowed')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('waits for the networks', async () => {
    mockAdmin()
    server.use(
      http.get('/api/v1/admin/networks', async () => {
        await delay('infinite')
        return HttpResponse.json({ fixed: [], custom: [] })
      }),
    )
    await renderApp({ path: '/admin' })
    await userEvent.setup().click(await screen.findByRole('tab', { name: 'Networks' }))

    expect(await screen.findByText('Loading networks…')).toBeInTheDocument()
  })

  it('shows the limits in force and what the server started with', async () => {
    mockAdmin({ limits: { ...defaultLimits, bandwidthGlobal: 5 * 1024 * 1024, dailyBytesPerUser: 1.5 * 1024 ** 3, transfersGlobal: 10 } })
    await renderApp({ path: '/admin' })
    await userEvent.setup().click(await screen.findByRole('tab', { name: 'Limits' }))

    expect(await screen.findByLabelText('Whole server', { selector: '#limit-bandwidthGlobal' })).toHaveValue('5')
    expect(screen.getByLabelText('Volume per person and day')).toHaveValue('1.5')
    expect(screen.getByLabelText('Per person', { selector: '#limit-bandwidthPerUser' })).toHaveValue('')
    expect(screen.getByLabelText('Per person', { selector: '#limit-bandwidthPerUser' })).toHaveAttribute('placeholder', 'Unlimited')
    expect(screen.getByLabelText('Whole server', { selector: '#limit-transfersGlobal' })).toHaveValue('10')
    expect(screen.getByText('Started with 32')).toBeInTheDocument()
    expect(screen.getAllByText('Started without a limit').length).toBeGreaterThan(0)
  })

  it('saves the limits in the units of the server', async () => {
    const mock = mockAdmin()
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: 'Limits' }))

    await user.type(await screen.findByLabelText('Whole server', { selector: '#limit-bandwidthGlobal' }), '2.5')
    await user.type(screen.getByLabelText('Sites per workspace'), '20')
    await user.type(screen.getByLabelText('Volume per person and day'), '0.5')
    await user.clear(screen.getByLabelText('Whole server', { selector: '#limit-transfersGlobal' }))
    await user.click(screen.getByRole('button', { name: 'Save limits' }))

    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')
    expect(mock.calls).toEqual([
      {
        method: 'PUT',
        path: '/api/v1/admin/limits',
        body: { ...defaultLimits, bandwidthGlobal: 2.5 * 1024 * 1024, maxSitesPerWorkspace: 20, dailyBytesPerUser: 0.5 * 1024 ** 3, transfersGlobal: 0 },
      },
    ])
    expect(screen.getByLabelText('Whole server', { selector: '#limit-bandwidthGlobal' })).toHaveValue('2.5')
    expect(screen.getByLabelText('Whole server', { selector: '#limit-transfersGlobal' })).toHaveValue('')
  })

  it('refuses what is not an amount before asking the server', async () => {
    const mock = mockAdmin()
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: 'Limits' }))

    await user.type(await screen.findByLabelText('Sites per workspace'), 'lots')
    await user.click(screen.getByRole('button', { name: 'Save limits' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('A limit is not valid')
    await user.clear(screen.getByLabelText('Sites per workspace'))
    await user.type(screen.getByLabelText('Sites per workspace'), '-3')
    await user.click(screen.getByRole('button', { name: 'Save limits' }))
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(mock.calls).toEqual([])
  })

  it('explains a refusal of the server and fills in the starting values on request', async () => {
    mockAdmin({ limits: { ...defaultLimits, transfersGlobal: 3, maxTeamsPerUser: 9 } })
    failAdmin('put', '/limits', 422, 'invalid_limits')
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: 'Limits' }))

    expect(await screen.findByLabelText('Teams a person owns')).toHaveValue('9')
    await user.click(screen.getByRole('button', { name: 'Save limits' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('A limit is not valid')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Fill in the starting values' }))
    expect(screen.getByLabelText('Teams a person owns')).toHaveValue('')
    expect(screen.getByLabelText('Whole server', { selector: '#limit-transfersGlobal' })).toHaveValue('32')
  })

  it('waits for the limits', async () => {
    mockAdmin()
    server.use(
      http.get('/api/v1/admin/limits', async () => {
        await delay('infinite')
        return HttpResponse.json({})
      }),
    )
    await renderApp({ path: '/admin' })
    await userEvent.setup().click(await screen.findByRole('tab', { name: 'Limits' }))

    expect(await screen.findByText('Loading limits…')).toBeInTheDocument()
  })

  it('words every action of the audit log', async () => {
    mockAdmin({
      audit: () => [
        event(6, 'admin.user_disabled', 'bob@acme.dev'),
        event(5, 'admin.user_enabled', 'bob@acme.dev'),
        event(4, 'admin.role_changed', 'ana@acme.dev', { meta: { role: 'admin' } }),
        event(3, 'admin.role_changed', 'ana@acme.dev', { meta: { role: 'user' } }),
        event(2, 'admin.second_factor_reset', 'ana@acme.dev'),
        event(1, 'admin.networks_changed', '', { meta: { count: '2' } }),
        event(0, 'admin.networks_changed', '', { meta: { count: '1' } }),
        event(-2, 'admin.limits_changed', ''),
        event(-1, 'admin.thing', 'x', { actor: undefined, ip: undefined, meta: {} }),
      ],
    })
    await renderApp({ path: '/admin' })
    await userEvent.setup().click(await screen.findByRole('tab', { name: 'Audit log' }))

    const text = (await screen.findByRole('list', { name: 'Instance audit log' })).textContent ?? ''
    for (const sentence of [
      'disabled the account bob@acme.dev',
      'enabled the account bob@acme.dev',
      'made ana@acme.dev Administrator',
      'made ana@acme.dev User',
      'removed the second step of ana@acme.dev',
      'changed the allowed networks (2 networks)',
      'changed the allowed networks (1 network)',
      'changed the limits of the instance',
      'admin.thing x',
      'Deleted account',
    ]) {
      expect(text).toContain(sentence)
    }
  })

  it('narrows the audit log by group and pages', async () => {
    const page = Array.from({ length: 50 }, (_, i) => event(200 - i, 'admin.user_disabled', `u${i}@acme.dev`))
    const mock = mockAdmin({ audit: (query) => (query.get('before') === '151' ? [event(100, 'admin.user_enabled', 'last@acme.dev')] : page) })
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: 'Audit log' }))

    await user.click(await screen.findByRole('button', { name: 'Load more' }))
    expect(await screen.findByText(/last@acme.dev/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Show'), 'Second step')
    await waitFor(() => expect(mock.auditQueries.some((q) => q.get('action') === 'admin.second_')).toBe(true))
    expect(mock.auditQueries[0].get('action')).toBeNull()
  })

  it('explains an empty audit log', async () => {
    mockAdmin()
    await renderApp({ path: '/admin' })
    await userEvent.setup().click(await screen.findByRole('tab', { name: 'Audit log' }))

    expect(await screen.findByText('Nothing has been recorded yet.')).toBeInTheDocument()
  })

  it('waits for the audit log', async () => {
    mockAdmin()
    server.use(
      http.get('/api/v1/admin/audit', async () => {
        await delay('infinite')
        return HttpResponse.json([])
      }),
    )
    await renderApp({ path: '/admin' })
    await userEvent.setup().click(await screen.findByRole('tab', { name: 'Audit log' }))

    expect(await screen.findByText('Loading the audit log…')).toBeInTheDocument()
  })

  it('waits for the accounts', async () => {
    mockAdmin()
    server.use(
      http.get('/api/v1/admin/users', async () => {
        await delay('infinite')
        return HttpResponse.json([])
      }),
    )
    await renderApp({ path: '/admin' })

    expect(await screen.findByText('Loading accounts…')).toBeInTheDocument()
  })

  it.each([
    ['Accounts', '/users', 'maria@acme.dev'],
    ['Networks', '/networks', 'Set at startup'],
    ['Limits', '/limits', 'Bandwidth'],
    ['Audit log', '/audit', 'Nothing has been recorded yet.'],
  ])('explains a %s tab that could not load instead of loading forever, and retries', async (tab, path, loaded) => {
    const mock = mockAdmin()
    failAdmin('get', path, 503, 'network_error')
    await renderApp({ path: '/admin' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: tab }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server.')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    mockAdmin({ users: mock.users })
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText(loaded)).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('waits for the session', async () => {
    mockWorkspaceData()
    server.use(
      http.get('/api/v1/me', async () => {
        await delay('infinite')
        return HttpResponse.json(adminSession)
      }),
    )
    await renderWithProviders(<AdminPage />)

    expect(await screen.findByText('Loading…')).toBeInTheDocument()
  })

  it('refuses whoever is not an administrator', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, role: 'user' } })
    mockWorkspaceData()
    await renderApp({ path: '/admin' })

    expect(await screen.findByText('Only administrators of the instance can open this page.')).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Accounts' })).not.toBeInTheDocument()
  })

  it('offers the page in the account menu to administrators only', async () => {
    mockAdmin()
    await renderApp({ path: '/' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Account' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Administration' }))

    expect(await screen.findByRole('heading', { name: 'Administration' })).toBeInTheDocument()
  })

  it('does not offer the page to a regular account', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, role: 'user' } })
    mockWorkspaceData()
    await renderApp({ path: '/' })
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Account' }))

    expect(await screen.findByRole('menuitem', { name: 'Settings' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: 'Administration' })).not.toBeInTheDocument()
  })

  it('is translated to Spanish', async () => {
    mockAdmin()
    await renderApp({ path: '/admin', language: 'es' })

    expect(await screen.findByRole('heading', { name: 'Administración' })).toBeInTheDocument()
    expect(await screen.findByRole('list', { name: 'Cuentas' })).toBeInTheDocument()
  })

  it('maps actions and errors to their keys', () => {
    expect(adminActionKey('admin.user_disabled')).toBe('userDisabled')
    expect(adminActionKey('admin.nothing')).toBe('unknown')
    expect(adminErrorKeyOf(new Error('x'))).toBe('unknown')
  })
})
