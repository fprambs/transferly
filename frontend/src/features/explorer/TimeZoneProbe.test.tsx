import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Site, TimeZoneProbe as Probe } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { Explorer } from './Explorer'
import { TimeZoneProbe } from './TimeZoneProbe'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** A saved FTP site, with settings that saving its time zone must keep. */
const ftp: Site = {
  id: 's1',
  name: 'Legacy',
  protocol: 'ftp',
  ftpEncryption: 'plain',
  host: 'legacy.example.com',
  port: 21,
  username: 'deploy',
  authMethod: 'password',
  listTimezone: 'UTC',
  charset: 'windows-1252',
  speedLimitBps: 1048576,
  proxy: { kind: 'socks5', host: 'proxy.example.com', port: 1080 },
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** Answers the probe with a result, or with a problem when given none. */
function mockProbe(result: Probe | null, waitMs = 0): void {
  server.use(
    http.post('/api/v1/workspaces/:id/sites/s1/files/timezone-probe', async () => {
      if (waitMs > 0) {
        await delay(waitMs)
      }
      return result === null ? problem(422, 'time_zone_inconclusive') : HttpResponse.json(result)
    }),
  )
}

/** Renders the button for a saved site or, without one, for a quick connection. */
async function renderProbe(site: Site | null = ftp, onApplied = vi.fn()) {
  const user = userEvent.setup()
  await renderWithProviders(
    <div className="relative">
      <TimeZoneProbe workspaceId={workspaceId} siteId="s1" path="/upload" site={site ?? undefined} onApplied={onApplied} />
    </div>,
  )
  return { user, onApplied }
}

/**
 * Covers {@link TimeZoneProbe} and where {@link Explorer} shows it.
 *
 * Scenarios:
 * - a server with exact times says no zone is needed, and the message can be dismissed
 * - a whole-hour offset offers its zone for a saved site, which saves the site with everything else unchanged, tells the explorer to reload and
 *   says it was saved; a failing save is explained
 * - a quick connection is told to save the connection first, and an offset that is not a whole number of hours is left to the settings, both with
 *   the offset written with its sign
 * - a probe that fails is explained
 * - while the probe runs the button waits and the message cannot be dismissed
 * - the explorer offers the button for FTP sites and not for SFTP ones
 */
describe('time zone probe', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [ftp, { ...ftp, id: 's2', name: 'Secure', protocol: 'sftp' }] })
  })

  it('says no zone is needed for exact times, and can be dismissed', async () => {
    mockProbe({ exact: true, offsetMinutes: 0 })
    const { user } = await renderProbe()

    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))
    expect(await screen.findByRole('status')).toHaveTextContent('This server lists exact times in UTC. No time zone is needed.')
    await user.click(screen.getByRole('button', { name: 'Dismiss' }))

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('saves the suggested zone on a saved site, keeping every other setting of the site', async () => {
    mockProbe({ exact: false, offsetMinutes: -180, suggestedZone: 'Etc/GMT+3' })
    const bodies: unknown[] = []
    server.use(
      http.put('/api/v1/workspaces/:id/sites/s1', async ({ request }) => {
        bodies.push(await request.json())
        return HttpResponse.json({ ...ftp, listTimezone: 'Etc/GMT+3' })
      }),
    )
    const { user, onApplied } = await renderProbe()

    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))
    expect(await screen.findByText('The server prints times -03:00 from UTC.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Use Etc/GMT+3 for this site' }))

    expect(await screen.findByText('Saved. The listing now uses Etc/GMT+3.')).toBeInTheDocument()
    expect(bodies).toEqual([
      {
        name: 'Legacy',
        protocol: 'ftp',
        ftpEncryption: 'plain',
        host: 'legacy.example.com',
        port: 21,
        username: 'deploy',
        authMethod: 'password',
        listTimezone: 'Etc/GMT+3',
        charset: 'windows-1252',
        speedLimitBps: 1048576,
        proxy: { kind: 'socks5', host: 'proxy.example.com', port: 1080 },
      },
    ])
    expect(onApplied).toHaveBeenCalledTimes(1)
  })

  it('explains a save that fails', async () => {
    mockProbe({ exact: false, offsetMinutes: 120, suggestedZone: 'Etc/GMT-2' })
    server.use(http.put('/api/v1/workspaces/:id/sites/s1', () => problem(403, 'forbidden')))
    const { user, onApplied } = await renderProbe()

    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))
    await user.click(await screen.findByRole('button', { name: 'Use Etc/GMT-2 for this site' }))

    expect(await screen.findByText(/The time zone could not be detected/)).toBeInTheDocument()
    expect(onApplied).not.toHaveBeenCalled()
  })

  it('tells a quick connection to be saved and leaves odd offsets to the settings', async () => {
    mockProbe({ exact: false, offsetMinutes: -180, suggestedZone: 'Etc/GMT+3' })
    const { user } = await renderProbe(null)
    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))

    expect(await screen.findByText('Save this connection as a site to keep the time zone.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Use / })).not.toBeInTheDocument()

    mockProbe({ exact: false, offsetMinutes: 330 })
    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))
    expect(await screen.findByText('The server prints times +05:30 from UTC.')).toBeInTheDocument()
    expect(screen.getByText('Choose the time zone in the settings of the site.')).toBeInTheDocument()
  })

  it('explains a probe that fails', async () => {
    mockProbe(null)
    const { user } = await renderProbe()

    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))

    expect(await screen.findByRole('status')).toHaveTextContent('The times of the server do not match the clock, so no time zone fits.')
  })

  it('waits while the probe runs', async () => {
    mockProbe({ exact: true, offsetMinutes: 0 }, 200)
    const { user } = await renderProbe()

    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))

    expect(screen.getByRole('status')).toHaveTextContent('Writing a small file to read its time…')
    expect(screen.getByRole('button', { name: 'Detect the time zone of the server' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Dismiss' })).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('exact times'))
  })

  it('is offered for FTP sites and reloads the listing once the zone is saved', async () => {
    let listings = 0
    server.use(http.get('/api/v1/workspaces/:id/sites/:siteId/files', () => (listings++, HttpResponse.json([]))))
    mockProbe({ exact: false, offsetMinutes: -180, suggestedZone: 'Etc/GMT+3' })
    server.use(http.put('/api/v1/workspaces/:id/sites/s1', () => HttpResponse.json({ ...ftp, listTimezone: 'Etc/GMT+3' })))
    const user = userEvent.setup()

    await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/" onNavigate={vi.fn()} />)
    await screen.findByText('This folder is empty.')
    await user.click(screen.getByRole('button', { name: 'Detect the time zone of the server' }))
    await user.click(await screen.findByRole('button', { name: 'Use Etc/GMT+3 for this site' }))

    await waitFor(() => expect(listings).toBe(2))
  })

  it('is not offered for SFTP sites', async () => {
    server.use(http.get('/api/v1/workspaces/:id/sites/:siteId/files', () => HttpResponse.json([])))

    await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s2" path="/" onNavigate={vi.fn()} />)

    await screen.findByText('This folder is empty.')
    expect(within(screen.getByRole('region')).queryByRole('button', { name: 'Detect the time zone of the server' })).not.toBeInTheDocument()
  })
})
