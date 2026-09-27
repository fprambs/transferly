import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { Explorer } from './Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** A saved site the explorer browses. */
const prod: Site = {
  id: 's1',
  name: 'Prod',
  protocol: 'sftp',
  host: 'prod.example.com',
  port: 2222,
  username: 'deploy',
  authMethod: 'password',
  listTimezone: 'UTC',
  charset: 'utf-8',
  speedLimitBps: 0,
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** Builds an entry. */
function entry(name: string, overrides: Partial<RemoteEntry> = {}): RemoteEntry {
  return {
    name,
    path: `/www/${name}`,
    type: 'file',
    size: 1536,
    mode: '-rw-r--r--',
    modifiedAt: '2026-03-08T07:30:00Z',
    modifiedPrecision: 'minute',
    ...overrides,
  }
}

/** Answers the listing of the site with entries. */
function mockListing(entries: RemoteEntry[]): void {
  server.use(http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json(entries)))
}

/** Answers the listing with an unknown server key. */
function unknownKey() {
  return HttpResponse.json(
    { type: 'about:blank', title: 'x', status: 409, code: 'host_key_unknown', params: { keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc' } },
    { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
  )
}

/** Renders the explorer of the mock site at a path. */
async function renderExplorer(path = '/www', siteId = 's1', language: 'en' | 'es' = 'en') {
  const onNavigate = vi.fn()
  const user = userEvent.setup()
  await renderWithProviders(
    <Explorer workspaceId={workspaceId} siteId={siteId} path={path} onNavigate={onNavigate} />,
    language,
  )
  return { user, onNavigate }
}

/**
 * Covers {@link Explorer}, {@link EntryTable} and {@link Breadcrumbs}.
 *
 * Scenarios:
 * - a folder lists directories first with their kind, size, modification time in the account's time zone, and permissions
 * - times without a precise clock show only the day, and the time zone falls back to the browser's without a session
 * - an empty folder, a loading folder and every kind of entry are shown
 * - directories open with double click and Enter, files do not, and Backspace goes up
 * - the arrow keys, Home and End move the selection and other keys are ignored
 * - the path steps, the up button and the refresh button navigate and reload
 * - only the rows near the window are drawn in a long folder
 * - failures are explained and can be retried
 * - a new server key asks to be trusted and then lists the folder; a failure to trust is shown
 * - FTP and FTPS report no permissions, so their entries and details show a dash instead of nominal ones
 * - the dates follow the interface language
 * - the search button opens a search of the folder in view, and a chosen match navigates there
 */
describe('explorer', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [prod] })
  })

  it('lists the folder with directories first', async () => {
    mockListing([
      entry('zeta.txt', { size: 5 * 1024 ** 2 }),
      entry('assets', { type: 'directory', size: 0, mode: 'drwxr-xr-x' }),
      entry('alpha.txt', { modifiedAt: '2026-03-08T00:00:00Z', modifiedPrecision: 'day' }),
    ])

    await renderExplorer()

    const grid = await screen.findByRole('grid', { name: 'Files' })
    const rows = within(grid).getAllByRole('row')
    expect(rows).toHaveLength(4)
    expect(within(rows[1]).getByText('assets')).toBeInTheDocument()
    expect(within(rows[1]).getByText('—')).toBeInTheDocument()
    expect(within(rows[1]).getByText('drwxr-xr-x')).toBeInTheDocument()
    expect(within(rows[2]).getByText('alpha.txt')).toBeInTheDocument()
    expect(within(rows[2]).getByText('Mar 8, 2026')).toBeInTheDocument()
    expect(within(rows[3]).getByText('5.0 MB')).toBeInTheDocument()
    expect(within(rows[3]).getByText('Mar 8, 2026, 7:30 AM')).toBeInTheDocument()
    expect(screen.getByText('3 items')).toBeInTheDocument()
    expect(grid).toHaveAttribute('aria-rowcount', '4')
  })

  it('falls back to the browser time zone without a session', async () => {
    mockSession(null)
    mockListing([entry('a.txt')])

    await renderExplorer()

    expect(within(await screen.findByRole('grid')).getByText(/Mar 8, 2026/)).toBeInTheDocument()
  })

  it('shows dates in the time zone of the account', async () => {
    mockSession({ ...adminSession, user: { ...adminSession.user, timezone: 'Asia/Tokyo' } })
    mockListing([entry('a.txt')])

    await renderExplorer()

    expect(await screen.findByText('Mar 8, 2026, 4:30 PM')).toBeInTheDocument()
  })

  it('says when a folder is empty and, while it loads, that it is connecting', async () => {
    mockListing([])

    await renderExplorer()

    expect(screen.getByRole('status')).toHaveTextContent('Connecting to')
    expect(await screen.findByText('This folder is empty.')).toBeInTheDocument()
  })

  it('draws every kind of entry', async () => {
    mockListing([entry('link', { type: 'symlink' }), entry('sock', { type: 'other' }), entry('dir', { type: 'directory' }), entry('file')])

    await renderExplorer()

    expect(await screen.findAllByRole('row')).toHaveLength(5)
  })

  it('opens directories and not files, and goes up with Backspace', async () => {
    mockListing([entry('assets', { type: 'directory', path: '/www/assets' }), entry('index.html')])
    const { user, onNavigate } = await renderExplorer()
    const grid = await screen.findByRole('grid')

    await user.dblClick(within(grid).getByText('assets'))
    expect(onNavigate).toHaveBeenLastCalledWith('/www/assets')

    await user.dblClick(within(grid).getByText('index.html'))
    expect(onNavigate).toHaveBeenCalledTimes(1)

    const scroller = grid
    scroller.focus()
    await user.keyboard('{Home}{Enter}')
    expect(onNavigate).toHaveBeenLastCalledWith('/www/assets')

    await user.keyboard('{ArrowDown}{Enter}')
    expect(onNavigate).toHaveBeenCalledTimes(2)

    await user.keyboard('{Backspace}')
    expect(onNavigate).toHaveBeenLastCalledWith('/')
  })

  it('moves the selection with the keyboard', async () => {
    mockListing([entry('a'), entry('b'), entry('c')])
    const { user } = await renderExplorer()
    const grid = await screen.findByRole('grid')
    const scroller = grid
    const selected = () => within(grid).getByRole('row', { selected: true })
    scroller.focus()

    expect(selected()).toHaveTextContent('a')
    await user.keyboard('{ArrowDown}')
    expect(selected()).toHaveTextContent('b')
    await user.keyboard('{End}')
    expect(selected()).toHaveTextContent('c')
    await user.keyboard('{ArrowDown}')
    expect(selected()).toHaveTextContent('c')
    await user.keyboard('{ArrowUp}{ArrowUp}{ArrowUp}')
    expect(selected()).toHaveTextContent('a')
    await user.keyboard('x')
    expect(selected()).toHaveTextContent('a')
    await user.click(within(grid).getByText('c'))
    expect(selected()).toHaveTextContent('c')
    expect(scroller).toHaveAttribute('aria-activedescendant', 'entry-2')
  })

  it('navigates with the path steps, the up button and reloads with refresh', async () => {
    let listed = 0
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files', () => (listed++, HttpResponse.json([entry('a')]))))
    const { user, onNavigate } = await renderExplorer('/var/www')
    await screen.findByRole('grid')

    await user.click(screen.getByRole('button', { name: '/' }))
    expect(onNavigate).toHaveBeenLastCalledWith('/')
    await user.click(screen.getByRole('button', { name: 'var' }))
    expect(onNavigate).toHaveBeenLastCalledWith('/var')
    expect(screen.getByRole('button', { name: 'www' })).toHaveAttribute('aria-current', 'page')
    await user.click(screen.getByRole('button', { name: 'Up one level' }))
    expect(onNavigate).toHaveBeenLastCalledWith('/var')

    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(listed).toBe(2))
  })

  it('disables the up button at the root', async () => {
    mockListing([entry('a')])

    await renderExplorer('/')
    await screen.findByRole('grid')

    expect(screen.getByRole('button', { name: 'Up one level' })).toBeDisabled()
  })

  it('draws only the rows near the window in a long folder', async () => {
    mockListing(Array.from({ length: 500 }, (_, index) => entry(`file-${String(index).padStart(3, '0')}.txt`)))

    await renderExplorer()

    const grid = await screen.findByRole('grid')
    expect(grid).toHaveAttribute('aria-rowcount', '501')
    const drawn = within(grid).getAllByRole('row').length
    expect(drawn).toBeGreaterThan(5)
    expect(drawn).toBeLessThan(40)
  })

  it('explains a failure and retries', async () => {
    let attempts = 0
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => (++attempts === 1 ? problem(404, 'path_not_found') : HttpResponse.json([entry('a')]))),
    )
    const { user } = await renderExplorer()

    expect(await screen.findByRole('alert')).toHaveTextContent('This folder does not exist.')
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('grid')).toBeInTheDocument()
  })

  it('asks to trust a new server key and then lists the folder', async () => {
    let listed = 0
    let trusted: unknown
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => (++listed === 1 ? unknownKey() : HttpResponse.json([entry('a')]))),
      http.post('/api/v1/workspaces/:id/known-hosts', async ({ request }) => {
        trusted = await request.json()
        return HttpResponse.json({}, { status: 201 })
      }),
    )
    const { user } = await renderExplorer()

    expect(await screen.findByText('SHA256:abc')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Trust and connect' }))

    expect(await screen.findByRole('grid')).toBeInTheDocument()
    expect(trusted).toEqual({ host: 'prod.example.com', port: 2222, keyType: 'ssh-ed25519', fingerprint: 'SHA256:abc' })
  })

  it('shows a failure to trust the key', async () => {
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => unknownKey()),
      http.post('/api/v1/workspaces/:id/known-hosts', () => problem(403, 'vault_locked')),
    )
    const { user } = await renderExplorer()

    await user.click(await screen.findByRole('button', { name: 'Trust and connect' }))

    expect(await screen.findByText('Unlock the vault to use the saved credentials.')).toBeInTheDocument()
  })

  it('cannot offer to trust a connection it does not know', async () => {
    server.use(http.get('/api/v1/workspaces/:id/sites/gone/files', () => unknownKey()))

    await renderExplorer('/www', 'gone')

    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong. Try again.')
    expect(screen.queryByRole('button', { name: 'Trust and connect' })).not.toBeInTheDocument()
  })

  it('shows a dash instead of permissions on an FTP site', async () => {
    mockWorkspaceData({ sites: [{ ...prod, protocol: 'ftp' }] })
    mockListing([entry('a.txt')])

    await renderExplorer()

    const grid = await screen.findByRole('grid')
    await waitFor(() => expect(within(grid).queryByText('-rw-r--r--')).not.toBeInTheDocument())
    expect(within(grid).getAllByText('—').length).toBeGreaterThan(0)
    expect(within(screen.getByRole('complementary')).queryByText('-rw-r--r--')).not.toBeInTheDocument()
  })

  it('formats dates in the interface language', async () => {
    mockListing([entry('a.txt')])

    await renderExplorer('/www', 's1', 'es')

    expect(within(await screen.findByRole('grid')).getByText(/8 mar 2026/)).toBeInTheDocument()
  })

  it('opens a search of the folder in view and navigates to a chosen match', async () => {
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', ({ request }) =>
        HttpResponse.json(
          new URL(request.url).searchParams.get('path') === '/www'
            ? [entry('assets', { type: 'directory', path: '/www/assets' }), entry('report-here.csv')]
            : [entry('deep-report.csv', { path: '/www/assets/deep-report.csv' })],
        ),
      ),
    )
    const { user, onNavigate } = await renderExplorer()
    await screen.findByRole('grid')

    await user.click(screen.getByRole('button', { name: 'Search' }))
    const dialog = screen.getByRole('dialog', { name: 'Search' })
    await user.type(within(dialog).getByLabelText('Search query'), 'report')
    await user.click(within(dialog).getByRole('button', { name: 'Search' }))
    await user.click(await within(dialog).findByText('/www/assets/deep-report.csv'))
    expect(onNavigate).toHaveBeenCalledWith('/www/assets')
    expect(screen.queryByRole('dialog', { name: 'Search' })).not.toBeInTheDocument()

    onNavigate.mockClear()
    await user.click(screen.getByRole('button', { name: 'Search' }))
    const again = screen.getByRole('dialog', { name: 'Search' })
    await user.type(within(again).getByLabelText('Search query'), 'report')
    await user.click(within(again).getByRole('button', { name: 'Search' }))
    await user.click(await within(again).findByText('/www/report-here.csv'))
    expect(onNavigate).not.toHaveBeenCalled()
  })

  it('opens a file in the remote editor with a double click, saves it and reloads the folder on close', async () => {
    mockListing([entry('notes.txt', { size: 5 })])
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files/content', () => HttpResponse.text('hello')),
      http.get('/api/v1/workspaces/:id/sites/s1/files/entry', () => HttpResponse.json(entry('notes.txt', { size: 5 }))),
      http.put('/api/v1/workspaces/:id/sites/s1/files/content', () => HttpResponse.json(entry('notes.txt', { size: 5 }), { status: 201 })),
    )
    const { user } = await renderExplorer()
    const grid = await screen.findByRole('grid')

    await user.dblClick(within(grid).getByText('notes.txt'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('/www/notes.txt')).toBeInTheDocument()

    await waitFor(() => expect(within(dialog).getByRole('textbox')).toHaveTextContent('hello'))
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(within(dialog).getByRole('button', { name: 'Save' })).toBeEnabled())

    await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(await screen.findByRole('grid')).toBeInTheDocument()
  })
})
