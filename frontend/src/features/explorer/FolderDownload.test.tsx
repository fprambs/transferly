import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose } from '@/test/select'
import { renderWithProviders } from '@/test/render'

import { Explorer } from './Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** Builds an entry below a folder. */
function entry(folder: string, name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `${folder}/${name}`, type, size: 10, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'second' }
}

/** Answers the listings of the folder in view, of the folder to download and of its subfolder. */
function mockListings(docs: () => Response = () => HttpResponse.json([entry('/www/docs', 'a.txt'), entry('/www/docs', 'sub', 'directory')])): void {
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', ({ request }) => {
      const path = new URL(request.url).searchParams.get('path')
      if (path === '/www') {
        return HttpResponse.json([entry('/www', 'docs', 'directory'), entry('/www', 'readme.txt')])
      }
      return path === '/www/docs/sub' ? HttpResponse.json([entry('/www/docs/sub', 'b.txt')]) : docs()
    }),
  )
}

/** Renders the explorer of the mock site and records the links the browser is asked to follow. */
async function renderExplorer() {
  const links: string[] = []
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    links.push(this.href)
  })
  const user = userEvent.setup()
  await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/www" onNavigate={vi.fn()} />)
  const grid = await screen.findByRole('grid')
  return { user, grid, links }
}

/**
 * Covers the download of folders in {@link Explorer}, {@link EntryTable} and {@link Inspector}.
 *
 * Scenarios:
 * - a folder downloads as a ZIP by default, from the inspector and from the context menu, through the archive address with a transfer identifier
 * - the context menu and the inspector also offer to download the files one by one, which start a download per file of the tree, in order
 * - a folder with more files than the limit, and a folder that cannot be read, are explained and the message can be dismissed
 * - a file can have its checksum computed with the algorithm chosen, shown and copied, a failure is reported, changing the algorithm or the file
 *   clears it, and folders offer none
 * - files keep offering their plain download and no folder items
 */
describe('folder downloads', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  afterEach(() => vi.restoreAllMocks())

  it('downloads a folder as a ZIP from the inspector and the context menu', async () => {
    mockListings()
    const { user, grid, links } = await renderExplorer()

    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'Download as ZIP' }))
    fireEvent.contextMenu(within(grid).getByText('docs'))
    await user.click(await screen.findByRole('menuitem', { name: 'Download as ZIP' }))

    expect(links).toHaveLength(2)
    for (const link of links) {
      const url = new URL(link)
      expect(url.pathname).toBe(`/api/v1/workspaces/${workspaceId}/sites/s1/files/archive`)
      expect(url.searchParams.get('path')).toBe('/www/docs')
      expect(url.searchParams.get('transferId')).toMatch(/^[0-9a-f-]{36}$/)
    }
    expect(links[0]).not.toBe(links[1])
  })

  it('downloads the files of a folder one by one', async () => {
    mockListings()
    const { user, grid, links } = await renderExplorer()

    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'One by one' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Starting the download of 2 files…')
    await waitFor(() => expect(links).toHaveLength(2))
    expect(links.map((link) => new URL(link).searchParams.get('path'))).toEqual(['/www/docs/a.txt', '/www/docs/sub/b.txt'])
    expect(new URL(links[0]).pathname).toBe(`/api/v1/workspaces/${workspaceId}/sites/s1/files/content`)
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())

    fireEvent.contextMenu(within(grid).getByText('docs'))
    await user.click(await screen.findByRole('menuitem', { name: 'Download files one by one' }))
    await waitFor(() => expect(links).toHaveLength(4))
  })

  it('explains a folder with too many files and one that cannot be read', async () => {
    const many = Array.from({ length: 501 }, (_, index) => entry('/www/docs', `f${index}.txt`))
    mockListings(() => HttpResponse.json(many))
    const { user, links } = await renderExplorer()
    const each = () => user.click(within(screen.getByRole('complementary')).getByRole('button', { name: 'One by one' }))

    await each()
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('This folder holds more than 500 files. Download it as a ZIP instead.')
    await user.click(within(alert).getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    mockListings(() => problem(404, 'path_not_found'))
    await each()
    expect(await screen.findByRole('alert')).toHaveTextContent('The files could not be downloaded: This folder does not exist.')
    expect(links).toEqual([])
  })

  it('computes and copies the checksum of a file', async () => {
    mockListings()
    const sums: Array<string | null> = []
    let answer: () => Response = () => HttpResponse.json({ algorithm: 'sha256', checksum: 'abc123', size: 10 })
    server.use(
      http.get('/api/v1/workspaces/:id/sites/s1/files/checksum', ({ request }) => {
        sums.push(new URL(request.url).searchParams.get('algorithm'))
        return answer()
      }),
    )
    const written: string[] = []
    const { user, grid } = await renderExplorer()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: (text: string) => (written.push(text), Promise.resolve()) } })
    await user.click(within(grid).getByText('readme.txt'))
    const inspector = within(screen.getByRole('complementary'))

    await user.click(inspector.getByRole('button', { name: 'Checksum' }))
    expect(await inspector.findByLabelText('Checksum value')).toHaveTextContent('abc123')
    await user.click(inspector.getByRole('button', { name: 'Copy the checksum' }))
    expect(written).toEqual(['abc123'])
    expect(await inspector.findByRole('button', { name: 'Checksum copied' })).toBeInTheDocument()

    await choose(user, inspector.getByRole('combobox', { name: 'Checksum algorithm' }), 'MD5')
    expect(inspector.queryByLabelText('Checksum value')).not.toBeInTheDocument()
    answer = () => problem(502, 'remote_unavailable')
    await user.click(inspector.getByRole('button', { name: 'Checksum' }))
    expect(await inspector.findByRole('alert')).toHaveTextContent('The checksum could not be computed.')
    expect(sums).toEqual(['sha256', 'md5'])

    await user.click(within(grid).getByText('docs'))
    expect(within(screen.getByRole('complementary')).queryByRole('button', { name: 'Checksum' })).not.toBeInTheDocument()
  })

  it('shows that the checksum is being read', async () => {
    mockListings()
    server.use(http.get('/api/v1/workspaces/:id/sites/s1/files/checksum', async () => (await new Promise((resolve) => setTimeout(resolve, 200)), HttpResponse.json({ algorithm: 'sha256', checksum: 'late', size: 1 }))))
    const { user, grid } = await renderExplorer()
    await user.click(within(grid).getByText('readme.txt'))
    const inspector = within(screen.getByRole('complementary'))

    await user.click(inspector.getByRole('button', { name: 'Checksum' }))

    expect(inspector.getByRole('status')).toHaveTextContent('Reading the file…')
    expect(inspector.getByRole('button', { name: 'Checksum' })).toBeDisabled()
    expect(await inspector.findByLabelText('Checksum value')).toHaveTextContent('late')
  })

  it('keeps the plain download for files and offers no folder items there', async () => {
    mockListings()
    const { user, grid } = await renderExplorer()

    await user.click(within(grid).getByText('readme.txt'))
    const inspector = within(screen.getByRole('complementary'))
    expect(inspector.getByRole('button', { name: 'Download' })).toBeInTheDocument()
    expect(inspector.queryByRole('button', { name: 'One by one' })).not.toBeInTheDocument()
    fireEvent.contextMenu(within(grid).getByText('readme.txt'))
    await screen.findByRole('menuitem', { name: 'Download' })
    expect(screen.queryByRole('menuitem', { name: 'Download files one by one' })).not.toBeInTheDocument()
    await user.keyboard('{Escape}')
  })
})
