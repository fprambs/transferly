import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { useState } from 'react'
import { beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry, Site } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { Explorer } from './Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** Builds a saved site. */
function site(id: string, name: string): Site {
  return {
    id,
    name,
    protocol: 'sftp',
    host: `${id}.example.com`,
    port: 22,
    username: 'u',
    authMethod: 'password',
    listTimezone: 'UTC',
    charset: 'utf-8',
    speedLimitBps: 0,
    secrets: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

/** Builds an entry in a folder. */
function entry(dir: string, name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `${dir}/${name}`, type, size: 10, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'second' }
}

/** Answers a folder listing of a site after a wait, so the loading state can be seen. */
function slow(siteId: string, path: string, entries: RemoteEntry[], wait: number) {
  return http.get(`/api/v1/workspaces/:id/sites/${siteId}/files`, async ({ request }) => {
    if (new URL(request.url).searchParams.get('path') === path) {
      await delay(wait)
      return HttpResponse.json(entries)
    }
    return undefined
  })
}

/** An explorer with the buttons a test needs to change the site, since the explorer takes its place from props. */
function Harness({ initialPath }: { initialPath: string }) {
  const [path, setPath] = useState(initialPath)
  const [siteId, setSiteId] = useState('s1')
  return (
    <>
      <button onClick={() => setSiteId('s2')}>other site</button>
      <Explorer workspaceId={workspaceId} siteId={siteId} path={path} onNavigate={setPath} />
    </>
  )
}

/**
 * Covers the loading states of the explorer: {@link ConnectingState} and the loading bar.
 *
 * Scenarios:
 * - the first listing of a connection shows an animated connecting message with the site's name and placeholder rows
 * - moving to another folder keeps the folder in view, dimmed and not clickable, with a loading bar that names the path,
 *   until the new folder arrives; the bar is then idle
 * - a folder that is empty is not called empty while another one is loading
 * - moving to another site never shows the files of the previous one
 */
describe('explorer loading', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData({ sites: [site('s1', 'Prod'), site('s2', 'Staging')] })
  })

  it('shows the connection being opened with the name of the site', async () => {
    server.use(slow('s1', '/www', [entry('/www', 'a.txt')], 300))
    await renderWithProviders(<Harness initialPath="/www" />)

    const status = await screen.findByRole('status')
    await waitFor(() => expect(status).toHaveTextContent('Connecting to Prod…'))
    expect(status).toHaveTextContent('Opening the connection and reading the first folder')
    expect(document.querySelectorAll('[data-slot="skeleton"]').length).toBeGreaterThan(5)
    expect(await screen.findByRole('grid')).toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('keeps the folder in view, dimmed, while another one loads', async () => {
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', async ({ request }) => {
        const path = new URL(request.url).searchParams.get('path')
        if (path === '/www/assets') {
          await delay(1000)
          return HttpResponse.json([entry('/www/assets', 'logo.svg')])
        }
        return HttpResponse.json([entry('/www', 'assets', 'directory')])
      }),
    )
    const user = userEvent.setup()
    await renderWithProviders(<Harness initialPath="/www" />)
    const grid = await screen.findByRole('grid')
    const bar = screen.getByLabelText(/Opening/)
    expect(bar).toHaveAttribute('aria-busy', 'false')

    await user.dblClick(within(grid).getByText('assets'))

    await waitFor(() => expect(screen.getByLabelText('Opening /www/assets…')).toHaveAttribute('aria-busy', 'true'))
    const dimmed = screen.getByRole('grid').closest('[aria-busy]') as HTMLElement
    expect(dimmed).toHaveAttribute('aria-busy', 'true')
    expect(dimmed).toHaveClass('pointer-events-none')
    expect(within(screen.getByRole('grid')).getByText('assets')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New folder' })).toBeDisabled()

    expect(await within(await screen.findByRole('grid')).findByText('logo.svg')).toBeInTheDocument()
    expect(screen.getByLabelText(/Opening/)).toHaveAttribute('aria-busy', 'false')
    expect(screen.getByRole('button', { name: 'New folder' })).toBeEnabled()
  })

  it('does not call a folder empty while another one loads', async () => {
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', async ({ request }) => {
        if (new URL(request.url).searchParams.get('path') === '/') {
          await delay(300)
          return HttpResponse.json([entry('', 'top', 'directory')])
        }
        return HttpResponse.json([])
      }),
    )
    const user = userEvent.setup()
    await renderWithProviders(<Harness initialPath="/empty" />)
    expect(await screen.findByText('This folder is empty.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Up one level' }))

    await waitFor(() => expect(screen.getByLabelText('Opening /…')).toHaveAttribute('aria-busy', 'true'))
    expect(screen.queryByText('This folder is empty.')).not.toBeInTheDocument()
    expect(await within(await screen.findByRole('grid')).findByText('top')).toBeInTheDocument()
  })

  it('never shows the files of the previous site while another one connects', async () => {
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([entry('/www', 'from-prod.txt')])),
      slow('s2', '/www', [entry('/www', 'from-staging.txt')], 300),
    )
    const user = userEvent.setup()
    await renderWithProviders(<Harness initialPath="/www" />)
    expect(await within(await screen.findByRole('grid')).findByText('from-prod.txt')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'other site' }))

    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Connecting to Staging…'))
    expect(screen.queryByText('from-prod.txt')).not.toBeInTheDocument()
    expect(await within(await screen.findByRole('grid')).findByText('from-staging.txt')).toBeInTheDocument()
  })
})
