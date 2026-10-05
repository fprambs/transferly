import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Site } from '@/lib/api'
import { FakeSocket, fakeSocketTransport } from '@/test/fake-socket'
import { adminSession, mockServerTools, mockSession, mockWorkspaceData, noTools } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 24
    loadAddon(): void {}
    open(): void {}
    focus(): void {}
    write(): void {}
    onData() {
      return { dispose: () => undefined }
    }
    onResize() {
      return { dispose: () => undefined }
    }
    dispose(): void {}
  },
}))

vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit(): void {} } }))

/** A saved SFTP site, with its terminal on unless told otherwise. */
function shellSite(id: string, name: string, terminal = 'true'): Site {
  return {
    connector: 'sftp', config: { host: `${id}.example.com`, port: '22', username: 'deploy', auth: 'password', terminal },
    id, name, speedLimitBps: 0, secrets: [], createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
  }
}

/** The terminal sockets opened so far. */
function terminals(): FakeSocket[] {
  return FakeSocket.opened.filter((socket) => socket.url.includes('/terminal'))
}

/**
 * Covers opening terminals from the explorer of a site (ADR-0027).
 *
 * Scenarios:
 * - a site with its terminal on, on a server that runs commands, offers the Terminal button; it opens a terminal in the folder in
 *   view under the explorer, another opens beside it; closing one that is not in view keeps the one in view, closing the one in view
 *   shows the last one left, closing the last closes the panel, and so does closing them all
 * - a site with its terminal off, or a server that runs no commands, offers no button
 */
describe('site terminals', () => {
  beforeEach(() => {
    FakeSocket.reset()
    mockSession(adminSession)
    server.use(http.get('/api/v1/workspaces/:id/sites/:siteId/files', () => HttpResponse.json([])))
  })
  afterEach(() => FakeSocket.reset())

  it('opens terminals in the folder in view', async () => {
    mockWorkspaceData({ sites: [shellSite('s1', 'Shell')] })
    mockServerTools({ ...noTools, shell: true })
    const user = userEvent.setup()
    await renderApp({ path: '/sites/s1?path=%2Fsrv', transport: fakeSocketTransport() })

    await user.click(await screen.findByRole('button', { name: 'Terminal' }, { timeout: 5_000 }))
    const panel = await screen.findByRole('region', { name: 'Terminals' })
    expect(terminals()[0].url).toContain('/api/v1/workspaces/')
    expect(terminals()[0].url).toContain('/sites/s1/terminal?path=%2Fsrv')
    await user.click(within(panel).getByRole('button', { name: 'New terminal in this folder' }))
    expect(within(panel).getAllByRole('tab')).toHaveLength(2)
    expect(within(panel).getAllByRole('tab')[1]).toHaveAttribute('aria-selected', 'true')
    await user.click(within(panel).getByRole('button', { name: 'Close Terminal 1' }))
    expect(within(panel).getByRole('tab')).toHaveTextContent('Terminal 2')
    expect(within(panel).getByRole('tab')).toHaveAttribute('aria-selected', 'true')
    await user.click(within(panel).getByRole('button', { name: 'New terminal in this folder' }))
    await user.click(within(panel).getAllByRole('tab')[0])
    expect(within(panel).getAllByRole('tab')[0]).toHaveAttribute('aria-selected', 'true')
    await user.click(within(panel).getByRole('button', { name: 'Close Terminal 2' }))
    expect(within(panel).getByRole('tab')).toHaveTextContent('Terminal 3')
    expect(within(panel).getByRole('tab')).toHaveAttribute('aria-selected', 'true')
    await user.click(within(panel).getByRole('button', { name: 'Close Terminal 3' }))
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Terminals' })).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Terminal' }))
    await user.click(within(await screen.findByRole('region', { name: 'Terminals' })).getByRole('button', { name: 'Close the terminals' }))
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Terminals' })).not.toBeInTheDocument())
  }, 20_000)

  it('offers no terminal where it cannot open', async () => {
    mockWorkspaceData({ sites: [shellSite('s1', 'Plain', 'false')] })
    mockServerTools({ ...noTools, shell: true })
    await renderApp({ path: '/sites/s1?path=%2F', transport: fakeSocketTransport() })
    await screen.findByRole('button', { name: 'New folder' })
    expect(screen.queryByRole('button', { name: 'Terminal' })).not.toBeInTheDocument()
  })
})
