import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { adminSession, mockSession } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { SearchDialog } from './SearchDialog'

/** Workspace and site the mock server answers for. */
const workspaceId = 'w1'
const siteId = 's1'

/** Builds a file or folder entry at an absolute path. */
function entry(path: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name: path.slice(path.lastIndexOf('/') + 1), path, type, size: 42, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second' }
}

/** Answers the listing of the site with a tree keyed by folder path, so a search actually walks it. */
function mockTree(tree: Record<string, RemoteEntry[]>): void {
  server.use(http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files`, ({ request }) => HttpResponse.json(tree[new URL(request.url).searchParams.get('path') ?? '/'] ?? [])))
}

/**
 * Covers {@link SearchDialog} and the hook it is built on, {@link useSearch}.
 *
 * Scenarios:
 * - a search finds a file directly in the folder and one nested inside a subfolder that does not itself match, in the order they were found
 * - the search button is disabled for a blank query, typing enables it, and submitting a blank query anyway starts nothing
 * - opening a match navigates to the folder that holds it and closes the dialog; opening a matching folder opens it directly
 * - stopping a search keeps what was found and ends it
 * - a folder that already matches nothing says so
 * - the count of matches follows the pluralization of the language, and a truncated search says there is more
 */
describe('search', () => {
  beforeEach(() => {
    mockSession(adminSession)
  })

  it('finds a file directly in the folder and one nested inside a subfolder, in order', async () => {
    mockTree({
      '/www': [entry('/www/report.csv'), entry('/www/notes.txt'), entry('/www/archive', 'directory')],
      '/www/archive': [entry('/www/archive/old-report.csv')],
    })
    const user = userEvent.setup()
    const onOpen = vi.fn()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={onOpen} onClose={() => {}} />)

    await user.type(screen.getByLabelText('Search query'), 'report')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    const list = await screen.findByRole('list', { name: 'Search results' })
    await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(2))
    const rows = within(list).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('/www/report.csv')
    expect(rows[1]).toHaveTextContent('/www/archive/old-report.csv')
    expect(await screen.findByText('2 matches found.')).toBeInTheDocument()
  })

  it('disables the search button for a blank query', async () => {
    mockTree({ '/www': [] })
    const user = userEvent.setup()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={() => {}} onClose={() => {}} />)

    expect(screen.getByRole('button', { name: 'Search' })).toBeDisabled()
    await user.type(screen.getByLabelText('Search query'), 'x')
    expect(screen.getByRole('button', { name: 'Search' })).toBeEnabled()
  })

  it('does not start a search submitted with a blank query', async () => {
    mockTree({ '/www': [entry('/www/a.txt')] })
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={() => {}} onClose={() => {}} />)

    fireEvent.submit(screen.getByLabelText('Search query').closest('form') as HTMLFormElement)

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('opens the folder of a matching file, closing the dialog', async () => {
    mockTree({ '/www': [entry('/www/reports', 'directory'), entry('/www/report.csv')], '/www/reports': [] })
    const user = userEvent.setup()
    const onOpen = vi.fn()
    const onClose = vi.fn()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={onOpen} onClose={onClose} />)
    await user.type(screen.getByLabelText('Search query'), 'report')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await user.click(await screen.findByText('/www/report.csv'))
    expect(onOpen).toHaveBeenCalledWith('/www')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('opens a matching folder directly, not its parent', async () => {
    mockTree({ '/www': [entry('/www/reports', 'directory')], '/www/reports': [] })
    const user = userEvent.setup()
    const onOpen = vi.fn()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={onOpen} onClose={() => {}} />)
    await user.type(screen.getByLabelText('Search query'), 'reports')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await user.click(await screen.findByText('/www/reports'))
    expect(onOpen).toHaveBeenCalledWith('/www/reports')
  })

  it('stops a search and keeps what was already found', async () => {
    let release = (): void => {}
    const blocked = new Promise<void>((resolve) => (release = resolve))
    server.use(
      http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files`, async ({ request }) => {
        const path = new URL(request.url).searchParams.get('path')
        if (path === '/www/slow') {
          await blocked
          return HttpResponse.json([])
        }
        return HttpResponse.json(path === '/www' ? [entry('/www/report.csv'), entry('/www/slow', 'directory')] : [])
      }),
    )
    const user = userEvent.setup()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={() => {}} onClose={() => {}} />)
    await user.type(screen.getByLabelText('Search query'), 'report')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await screen.findByText('/www/report.csv')
    await user.click(screen.getByRole('button', { name: 'Stop' }))
    expect(await screen.findByText('Stopped. 1 match found so far.')).toBeInTheDocument()
    release()
  })

  it('says so when nothing matches', async () => {
    mockTree({ '/www': [entry('/www/a.txt')] })
    const user = userEvent.setup()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={() => {}} onClose={() => {}} />)
    await user.type(screen.getByLabelText('Search query'), 'nothing-like-this')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('0 matches found.')).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Search results' })).not.toBeInTheDocument()
  })

  it('says there is more once the limit of matches is reached', async () => {
    mockTree({ '/www': Array.from({ length: 1005 }, (_, index) => entry(`/www/file-${index}.txt`)) })
    const user = userEvent.setup()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={() => {}} onClose={() => {}} />)
    await user.type(screen.getByLabelText('Search query'), 'file')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(await screen.findByText('1000 matches found. There is more; narrow the search to see everything.')).toBeInTheDocument()
  })

  it('is translated to Spanish and follows its pluralization', async () => {
    mockTree({ '/www': [entry('/www/a.txt')] })
    const user = userEvent.setup()
    await renderWithProviders(<SearchDialog workspaceId={workspaceId} siteId={siteId} path="/www" onOpen={() => {}} onClose={() => {}} />, 'es')
    await user.type(screen.getByLabelText('Texto de búsqueda'), 'a.txt')
    await user.click(screen.getByRole('button', { name: 'Buscar' }))

    expect(await screen.findByText('1 coincidencia encontrada.')).toBeInTheDocument()
  })
})
