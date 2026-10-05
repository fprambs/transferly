import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry, ServerTools, Site } from '@/lib/api'
import { adminSession, mockServerTools, mockSession, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { useNotices } from '../notices/notices-store'

import { archiveFormatOf, archiveName, isArchiveName } from './archive-formats'
import { Explorer } from './Explorer'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** A saved site whose server runs commands. */
const shell: Site = {
  connector: 'sftp', config: { host: 'shell.example.com', port: '22', username: 'deploy', auth: 'password' },
  id: 's1',
  name: 'Shell',
  speedLimitBps: 0,
  secrets: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/** What a server with every tool offers. */
const everything: ServerTools = { copy: true, checksums: ['sha256'], archive: ['zip', 'tar.gz'], extract: ['zip', 'tar.gz'], usage: true }

/** Builds an entry inside /www. */
function entry(name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `/www/${name}`, type, size: 2048, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'second' }
}

/** Answers the listing and the operations on the server, recording what they were asked. */
function mockServer() {
  const seen: string[] = []
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', () => HttpResponse.json([entry('site', 'directory'), entry('logs.tgz'), entry('broken.zip'), entry('notes.txt')])),
    http.post('/api/v1/workspaces/:id/sites/s1/files/compress', async ({ request }) => {
      const body = (await request.json()) as { name: string }
      seen.push(`compress ${JSON.stringify(body)}`)
      return body.name === 'full.zip' ? problem(507, 'remote_quota_exceeded') : HttpResponse.json(entry(body.name), { status: 201 })
    }),
    http.post('/api/v1/workspaces/:id/sites/s1/files/extract', async ({ request }) => {
      const body = (await request.json()) as { path: string }
      seen.push(`extract ${body.path}`)
      return body.path.endsWith('broken.zip') ? problem(422, 'server_commands_unsupported') : HttpResponse.json(entry('logs', 'directory'), { status: 201 })
    }),
    http.get('/api/v1/workspaces/:id/sites/s1/files/usage', ({ request }) => {
      const path = new URL(request.url).searchParams.get('path')
      seen.push(`usage ${path}`)
      return seen.filter((s) => s.startsWith('usage')).length === 1 ? problem(502, 'remote_unavailable') : HttpResponse.json({ bytes: 3 * 1024 * 1024 })
    }),
  )
  return seen
}

/** Renders the explorer and waits for the table. */
async function renderExplorer() {
  const user = userEvent.setup()
  await renderWithProviders(<Explorer workspaceId={workspaceId} siteId="s1" path="/www" onNavigate={vi.fn()} />)
  const grid = await screen.findByRole('grid')
  return { user, grid }
}

/** The texts of the notices shown so far. */
function notices(): string[] {
  return useNotices.getState().notices.map((notice) => notice.text)
}

/**
 * Covers the operations the server runs on its own (ADR-0027) in the explorer: {@link CompressDialog}, Extract here and the size of a folder.
 *
 * Scenarios:
 * - a server that cannot run commands offers none of them
 * - an entry is compressed from the context menu and from the inspector, in the format chosen, with a name that follows the format until it
 *   is edited; a name without the extension of the format is refused before asking, and a failure of the server is shown in the dialog
 * - an archive the server unpacks is extracted from the context menu and from the inspector, and the new folder is selected; a failure is
 *   told in a notice; files that are not archives offer no extraction
 * - the size of a folder is measured on request, and measured again after a failure
 */
describe('server commands', () => {
  beforeEach(() => {
    useNotices.setState({ notices: [], next: 1 })
    mockSession(adminSession)
    mockWorkspaceData({ sites: [shell] })
  })

  it('offers nothing when the server cannot run commands', async () => {
    mockServer()
    const { grid } = await renderExplorer()
    fireEvent.contextMenu(within(grid).getByText('logs.tgz'))
    await screen.findByRole('menuitem', { name: 'Rename' })
    expect(screen.queryByRole('menuitem', { name: 'Compress…' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: 'Extract here' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Measure' })).not.toBeInTheDocument()
  })

  it('compresses an entry on the server', async () => {
    mockServerTools(everything)
    const seen = mockServer()
    const { user, grid } = await renderExplorer()

    fireEvent.contextMenu(within(grid).getByText('site'))
    await user.click(await screen.findByRole('menuitem', { name: 'Compress…' }))
    let dialog = await screen.findByRole('dialog', { name: 'Compress site' })
    const name = within(dialog).getByLabelText('Archive name')
    expect(name).toHaveValue('site.zip')
    await user.click(within(dialog).getByLabelText('Format'))
    await user.click(await screen.findByRole('option', { name: 'tar.gz' }))
    expect(name).toHaveValue('site.tar.gz')
    await user.clear(name)
    await user.type(name, 'site.zip')
    await user.click(within(dialog).getByRole('button', { name: 'Compress' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('ends with .tar.gz')
    await user.click(within(dialog).getByLabelText('Format'))
    await user.click(await screen.findByRole('option', { name: 'ZIP' }))
    expect(name).toHaveValue('site.zip')
    await user.clear(name)
    await user.type(name, 'full.zip')
    await user.click(within(dialog).getByRole('button', { name: 'Compress' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The server has no space left.')
    await user.clear(name)
    await user.type(name, 'bundle.zip')
    await user.click(within(dialog).getByRole('button', { name: 'Compress' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(seen.at(-1)).toBe('compress {"dir":"/www","names":["site"],"name":"bundle.zip","format":"zip"}')
    expect(notices()).toEqual(['Compressed site into bundle.zip.'])

    await user.click(within(grid).getByText('notes.txt'))
    await user.click(within(screen.getByRole('complementary', { name: 'Details' })).getByRole('button', { name: 'Compress…' }))
    dialog = await screen.findByRole('dialog', { name: 'Compress notes.txt' })
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('extracts an archive on the server', async () => {
    mockServerTools(everything)
    const seen = mockServer()
    const { user, grid } = await renderExplorer()

    fireEvent.contextMenu(within(grid).getByText('notes.txt'))
    await screen.findByRole('menuitem', { name: 'Compress…' })
    expect(screen.queryByRole('menuitem', { name: 'Extract here' })).not.toBeInTheDocument()
    await user.keyboard('{Escape}')

    fireEvent.contextMenu(within(grid).getByText('logs.tgz'))
    await user.click(await screen.findByRole('menuitem', { name: 'Extract here' }))
    await waitFor(() => expect(notices()).toEqual(['Extracted logs.tgz into logs.']))
    expect(seen).toContain('extract /www/logs.tgz')

    await user.click(within(grid).getByText('broken.zip'))
    await user.click(within(screen.getByRole('complementary', { name: 'Details' })).getByRole('button', { name: 'Extract here' }))
    await waitFor(() => expect(notices().at(-1)).toBe('broken.zip could not be extracted: This server does not let Transferly run commands, or an administrator turned them off.'))
  })

  it('measures the size of a folder', async () => {
    mockServerTools(everything)
    const seen = mockServer()
    const { user } = await renderExplorer()
    const inspector = screen.getByRole('complementary', { name: 'Details' })

    await user.click(await within(inspector).findByRole('button', { name: 'Measure' }))
    expect(await within(inspector).findByRole('alert')).toHaveTextContent('The size could not be measured.')
    await user.click(within(inspector).getByRole('button', { name: 'Measure' }))
    expect(await within(inspector).findByText('3.0 MB')).toBeInTheDocument()
    expect(seen).toEqual(['usage /www/site', 'usage /www/site'])
  })
})

/**
 * Covers the names of archives.
 *
 * Scenarios:
 * - ZIP, tar.gz and tgz names tell their format in any case; other names and bare extensions tell none
 * - an archive is named after its entry with the first extension of its format, and a name fits a format only with its extension
 */
describe('archive formats', () => {
  it('tells and makes names of archives', () => {
    expect(archiveFormatOf('a.ZIP')).toBe('zip')
    expect(archiveFormatOf('a.tar.gz')).toBe('tar.gz')
    expect(archiveFormatOf('a.tgz')).toBe('tar.gz')
    expect(archiveFormatOf('.zip')).toBeUndefined()
    expect(archiveFormatOf('a.tar')).toBeUndefined()
    expect(archiveName('site', 'tar.gz')).toBe('site.tar.gz')
    expect(isArchiveName('a.tgz', 'tar.gz')).toBe(true)
    expect(isArchiveName('a/b.zip', 'zip')).toBe(false)
    expect(isArchiveName('a.zip', 'tar.gz')).toBe(false)
  })
})
