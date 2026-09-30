import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ArchiveEntry, ArchiveListing } from '@/lib/api'
import { ApiError } from '@/lib/transport'
import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { EntryTooLarge, type ArchiveAccess, type PreviewSource } from '../source'

import { ArchiveViewer } from './ArchiveViewer'

/** A failed request with a guided code. */
function failure(code: string): ApiError {
  return new ApiError({ type: 'about:blank', title: code, status: 422, code })
}

/** The entries of the project archive the tests browse. */
const entries: ArchiveEntry[] = [
  { name: 'project/', type: 'directory', size: 0, packed: 0, modified: '2026-09-30T12:00:00Z' },
  { name: 'project/README.md', type: 'file', size: 200, packed: 100, modified: '2026-09-30T12:00:00Z' },
  { name: 'project/src/main.go', type: 'file', size: 50, packed: -1 },
  { name: 'project/LATEST', type: 'symlink', size: 0, packed: -1 },
  { name: 'project/zeros.bin', type: 'file', size: 300 * 1024 * 1024, packed: 300 * 1024 },
  { name: '../escape.txt', type: 'file', size: 1, packed: 1 },
]

/** An archive whose listing and entries the test decides. */
function fakeArchive(listing: () => Promise<ArchiveListing>, open: (entry: ArchiveEntry) => Promise<PreviewSource>, download = true): ArchiveAccess {
  return { list: vi.fn(listing), open: vi.fn(open), downloadUrl: download ? (entry) => `/dl/${entry.name}` : undefined }
}

/** Renders the viewer of an archive named p.zip. */
async function show(archive: ArchiveAccess | undefined) {
  const source: PreviewSource = { ...bytesSource('p.zip', new TextEncoder().encode('PK\u0003\u0004')), archive }
  await renderWithProviders(<ArchiveViewer source={source} kind="archive" />)
  return userEvent.setup()
}

/** Opens every entry as a text file holding its name. */
const openAsText = async (entry: ArchiveEntry): Promise<PreviewSource> => bytesSource('entry.txt', new TextEncoder().encode(`text of ${entry.name}`))

/**
 * Covers {@link ArchiveViewer} (D2.5).
 *
 * Scenarios:
 * - a file with no way to list its entries shows as bytes
 * - the tree tells the format and totals, opens the lone top folder, shows sizes, compression and times, and marks unsafe names and
 *   likely bombs
 * - the arrows move the selection, Enter and Space open and close folders and open files, the chevron and a double click too
 * - a filter narrows the tree, and says when nothing matches; an empty archive says so; a truncated listing says so
 * - an opened entry shows with its own viewer under a bar with its path and download, and Backspace or the back button return to the tree,
 *   while Backspace typed in a field does not
 * - a listing or an entry that fails shows its guidance and can be tried again; an entry over the cap says it is too large
 */
describe('ArchiveViewer', () => {
  it('shows bytes when the file cannot be listed', async () => {
    await show(undefined)
    expect(await screen.findByRole('table', { name: 'Bytes of p.zip' })).toBeInTheDocument()
  })

  it('shows the tree of the archive', async () => {
    await show(fakeArchive(async () => ({ format: 'zip', entries, truncated: true }), openAsText))
    const tree = await screen.findByRole('tree', { name: 'Contents of p.zip' })
    expect(screen.getByText('zip')).toBeInTheDocument()
    expect(screen.getByText('5 files · 300.0 MB')).toBeInTheDocument()
    expect(screen.getByText('Only the first 6 entries are listed')).toBeInTheDocument()
    const readme = within(tree).getByRole('treeitem', { name: 'README.md' })
    expect(readme).toHaveTextContent('200 B')
    expect(readme).toHaveTextContent('50% packed')
    expect(readme).toHaveTextContent('2026')
    expect(within(tree).getByRole('treeitem', { name: 'src' })).toHaveAttribute('aria-expanded', 'false')
    expect(within(tree).getByRole('treeitem', { name: 'zeros.bin' })).toHaveTextContent('decompression bomb?')
    expect(within(tree).getByRole('treeitem', { name: '../escape.txt' })).toHaveTextContent('unsafe name')
  })

  it('walks the tree with the keyboard and the mouse', async () => {
    const archive = fakeArchive(async () => ({ format: 'zip', entries, truncated: false }), openAsText)
    const user = await show(archive)
    const tree = await screen.findByRole('tree')
    const row = (name: string) => within(tree).getByRole('treeitem', { name })
    tree.focus()
    await user.keyboard('x{ArrowDown}')
    expect(row('src')).toHaveAttribute('aria-selected', 'true')
    await user.keyboard('{Enter}')
    expect(row('src')).toHaveAttribute('aria-expanded', 'true')
    expect(row('main.go')).toBeInTheDocument()
    await user.keyboard('{ArrowUp}{ArrowUp}{Enter}')
    expect(row('project')).toHaveAttribute('aria-expanded', 'false')
    await user.click(within(row('project')).getByRole('button', { name: 'Expand' }))
    expect(row('project')).toHaveAttribute('aria-expanded', 'true')
    await user.dblClick(row('LATEST'))
    expect(archive.open).not.toHaveBeenCalled()
    await user.click(row('README.md'))
    tree.focus()
    await user.keyboard(' ')
    expect(await screen.findByLabelText('Contents of entry.txt')).toHaveTextContent('text of project/README.md')
    expect(archive.open).toHaveBeenCalledWith(entries[1])
  })

  it('filters the tree', async () => {
    const user = await show(fakeArchive(async () => ({ format: 'tar', entries, truncated: false }), openAsText))
    const filter = await screen.findByRole('textbox', { name: 'Filter by name' })
    await user.type(filter, 'main')
    expect(screen.getAllByRole('treeitem').map((item) => item.getAttribute('aria-label'))).toEqual(['project', 'src', 'main.go'])
    await user.type(filter, 'zzz')
    expect(screen.getByText('No entry matches the filter.')).toBeInTheDocument()
  })

  it('says when the archive is empty', async () => {
    await show(fakeArchive(async () => ({ format: 'zip', entries: [], truncated: false }), openAsText))
    expect(await screen.findByText('This archive is empty.')).toBeInTheDocument()
  })

  it('opens an entry and goes back to the tree', async () => {
    const user = await show(fakeArchive(async () => ({ format: 'zip', entries, truncated: false }), openAsText))
    const tree = await screen.findByRole('tree')
    await user.dblClick(within(tree).getByRole('treeitem', { name: 'README.md' }))
    expect(await screen.findByLabelText('Contents of entry.txt')).toHaveTextContent('text of project/README.md')
    expect(screen.getByText('project/README.md')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Download this entry' })).toHaveAttribute('href', '/dl/project/README.md')
    const field = document.createElement('input')
    screen.getByRole('button', { name: 'p.zip' }).parentElement?.append(field)
    field.focus()
    await user.keyboard('{Backspace}')
    expect(screen.getByText('project/README.md')).toBeInTheDocument()
    screen.getByRole('button', { name: 'p.zip' }).focus()
    await user.keyboard('{Backspace}')
    expect(await screen.findByRole('tree')).toBeVisible()
    await user.dblClick(within(tree).getByRole('treeitem', { name: 'README.md' }))
    await user.click(await screen.findByRole('button', { name: 'p.zip' }))
    expect(screen.queryByText('project/README.md')).not.toBeInTheDocument()
  })

  it('shows failures with their guidance', async () => {
    let listed = 0
    const archive = fakeArchive(
      async () => {
        listed++
        if (listed === 1) {
          throw failure('archive_unsupported')
        }
        return { format: 'zip', entries, truncated: false }
      },
      async (entry) => {
        if (entry.name === 'project/zeros.bin') {
          throw new EntryTooLarge()
        }
        throw failure('archive_encrypted')
      },
      false,
    )
    const user = await show(archive)
    expect(await screen.findByText('The archive could not be listed.')).toBeInTheDocument()
    expect(screen.getByText(/not an archive the viewer reads/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    const tree = await screen.findByRole('tree')
    await user.dblClick(within(tree).getByRole('treeitem', { name: 'README.md' }))
    expect(await screen.findByText('This entry could not be read.')).toBeInTheDocument()
    expect(screen.getByText(/protected with a password/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Download this entry' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    expect(archive.open).toHaveBeenCalledTimes(2)
    await user.click(screen.getByRole('button', { name: 'p.zip' }))
    await user.dblClick(within(tree).getByRole('treeitem', { name: 'zeros.bin' }))
    expect(await screen.findByText('The viewer opens entries up to 64.0 MB. Download it to open it on your computer.')).toBeInTheDocument()
  })
})
