import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { useState } from 'react'
import { beforeEach, describe, expect, it } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData, personalWorkspace } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose } from '@/test/select'
import { renderWithProviders } from '@/test/render'

import { Explorer } from './Explorer'

/** Builds an entry of a folder. */
function entry(folder: string, name: string, overrides: Partial<RemoteEntry> = {}): RemoteEntry {
  return { name, path: `${folder === '/' ? '' : folder}/${name}`, type: 'file', size: 10, mode: '-rw-r--r--', modifiedAt: new Date().toISOString(), modifiedPrecision: 'minute', ...overrides }
}

/** Answers the listings: /www holds a folder and four files, and any other folder one file; the listings are counted. */
function mockListings(): { count: number } {
  const asked = { count: 0 }
  server.use(
    http.get('/api/v1/workspaces/:id/sites/:siteId/files', ({ request }) => {
      asked.count++
      const folder = new URL(request.url).searchParams.get('path') ?? '/'
      if (folder !== '/www') {
        return HttpResponse.json([entry(folder, 'inner.txt')])
      }
      return HttpResponse.json([
        entry(folder, 'assets', { type: 'directory', size: 0 }),
        entry(folder, 'access.log'),
        entry(folder, 'error.log', { modifiedAt: '2020-01-01T00:00:00Z' }),
        entry(folder, 'photo.png', { size: 300 * 1024 * 1024 }),
        entry(folder, 'Canción.mp3'),
      ])
    }),
  )
  return asked
}

/** An explorer that keeps the folder it moves to, as the site view does. */
function Browsing() {
  const [path, setPath] = useState('/www')
  return <Explorer workspaceId={personalWorkspace.id} siteId="q1" path={path} onNavigate={setPath} />
}

/** Renders the explorer and waits for the listing. */
async function renderFiltered() {
  const user = userEvent.setup()
  await renderWithProviders(<Browsing />)
  const grid = await screen.findByRole('grid', { name: 'Files' })
  return { user, grid, box: screen.getByRole('searchbox', { name: 'Filter this folder' }) }
}

/** The names in view, in order. */
function names(grid: HTMLElement): string[] {
  return within(grid)
    .getAllByRole('row')
    .slice(1)
    .map((row) => (row.querySelector('[role="gridcell"]') as HTMLElement).textContent as string)
}

/**
 * Covers the instant filter of the explorer (4.14d): {@link FilterBox}, {@link FilterMenu} and {@link FilterSummary}.
 *
 * Scenarios:
 * - typing filters the folder at once, highlights the words and says how many entries it keeps of how many and how old the listing is,
 *   without asking the server again; the details follow what is in view
 * - Escape clears the text and then goes back to the list, as the down arrow does; the clear button clears it; / and Ctrl+F focus the box,
 *   and / focuses the box of the main pane when nothing else has focus
 * - the filters menu keeps groups of types, how recently entries changed, a range of days picked on its calendar and a size, counts the
 *   facets in use and clears them keeping the text; each facet is a chip of the summary that removes it
 * - a range may be open at its end, the same day ends it, and picking that day again clears it
 * - when nothing matches the explorer says so and clears the filter; the summary lists the folder again and clears everything
 * - moving to another folder starts without a filter
 */
describe('explorer filter', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  it('filters as the user types, without asking the server', async () => {
    const asked = mockListings()
    const { user, grid, box } = await renderFiltered()
    const before = asked.count

    await user.type(box, 'log')
    expect(names(grid)).toEqual(['access.log', 'error.log'])
    expect(within(grid).getAllByText('log', { selector: 'mark' })).toHaveLength(2)
    const summary = screen.getByRole('status', { name: '' })
    expect(summary).toHaveTextContent('2 of 5')
    expect(summary).toHaveTextContent(/Listed (now|\d+ seconds? ago)/)
    expect(within(screen.getByRole('complementary', { name: 'Details' })).getByText('/www/access.log')).toBeInTheDocument()

    await user.clear(box)
    await user.type(box, '*.log -error')
    expect(names(screen.getByRole('grid'))).toEqual(['access.log'])
    expect(asked.count).toBe(before)
    await user.click(within(screen.getByText('1 of 5').closest('[role="status"]') as HTMLElement).getByRole('button', { name: 'Clear filters' }))
    expect(box).toHaveValue('')
  })

  it('clears with Escape and the button, goes back to the list, and answers its shortcuts', async () => {
    mockListings()
    const { user, grid, box } = await renderFiltered()

    await user.type(box, 'photo')
    await user.keyboard('{Escape}')
    expect(box).toHaveValue('')
    expect(box).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(grid).toHaveFocus()

    await user.keyboard('/')
    expect(box).toHaveFocus()
    await user.type(box, 'log/')
    expect(box).toHaveValue('log/')
    await user.type(box, '{Backspace}')
    await user.keyboard('{ArrowDown}')
    expect(screen.getByRole('grid')).toHaveFocus()
    fireEvent.keyDown(screen.getByRole('grid'), { key: 'f', ctrlKey: true })
    expect(box).toHaveFocus()
    box.blur()
    fireEvent.keyDown(document.body, { key: 'x' })
    expect(box).not.toHaveFocus()
    fireEvent.keyDown(document.body, { key: '/' })
    expect(box).toHaveFocus()

    await user.click(screen.getByRole('button', { name: 'Clear the filter text' }))
    expect(box).toHaveValue('')
    expect(box).toHaveFocus()
    expect(names(screen.getByRole('grid'))).toHaveLength(5)
  })

  it('keeps types, dates and sizes from the filters menu, and removes each from the summary', async () => {
    mockListings()
    const { user, grid, box } = await renderFiltered()

    await user.click(screen.getByRole('button', { name: 'Filters, 0 in use' }))
    const menu = await screen.findByRole('dialog')
    expect(within(menu).getByRole('button', { name: 'Clear these filters' })).toBeDisabled()
    await user.click(within(menu).getByRole('button', { name: 'Documents' }))
    await user.click(within(menu).getByRole('button', { name: 'Audio and video' }))
    expect(names(grid)).toEqual(['access.log', 'Canción.mp3', 'error.log'])
    await user.click(within(menu).getByRole('button', { name: 'Audio and video' }))
    expect(within(menu).getByRole('button', { name: 'Documents' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(within(menu).getByRole('button', { name: 'Last 7 days' }))
    expect(names(grid)).toEqual(['access.log'])
    await user.click(within(menu).getByRole('button', { name: 'Between dates' }))
    await choose(user, await within(menu).findByRole('combobox', { name: 'Choose the Year' }), '2020')
    await choose(user, within(menu).getByRole('combobox', { name: 'Choose the Month' }), /January/i)
    await user.click(within(menu).getByRole('button', { name: /December 31st, 2019/ }))
    await user.click(within(menu).getByRole('button', { name: /January 2nd, 2020/ }))
    expect(within(menu).getByText('Dec 31, 2019')).toBeInTheDocument()
    expect(within(menu).getByText('Jan 2, 2020')).toBeInTheDocument()
    expect(screen.getByText('Dec 31, 2019 to Jan 2, 2020')).toBeInTheDocument()
    expect(names(grid)).toEqual(['error.log'])
    expect(screen.getByRole('button', { name: 'Filters, 2 in use' })).toBeInTheDocument()

    await user.click(within(menu).getByRole('button', { name: 'Clear these filters' }))
    expect(names(grid)).toHaveLength(5)
    await user.click(within(menu).getByRole('button', { name: 'Over 100 MB' }))
    expect(names(grid)).toEqual(['photo.png'])
    await user.click(within(menu).getByRole('button', { name: 'Folders' }))
    await user.click(within(menu).getByRole('button', { name: 'Last 30 days' }))
    await user.keyboard('{Escape}')
    await user.type(box, 'pho')

    const summary = screen.getByText('0 of 5').closest('[role="status"]') as HTMLElement
    await user.click(within(summary).getByRole('button', { name: 'Remove the filter: Type' }))
    expect(names(screen.getByRole('grid'))).toEqual(['photo.png'])
    await user.click(within(summary).getByRole('button', { name: 'Remove the filter: Size' }))
    await user.click(within(summary).getByRole('button', { name: 'Remove the filter: Modified' }))
    await user.click(within(summary).getByRole('button', { name: 'Remove the filter: Text' }))
    expect(names(screen.getByRole('grid'))).toHaveLength(5)
    expect(screen.queryByText(/of 5/)).not.toBeInTheDocument()
  })

  it('writes an open range and says when nothing matches', async () => {
    const asked = mockListings()
    const { user, grid, box } = await renderFiltered()

    await user.click(screen.getByRole('button', { name: 'Filters, 0 in use' }))
    const menu = await screen.findByRole('dialog')
    await user.click(within(menu).getByRole('button', { name: 'Between dates' }))
    expect(screen.getByText('… to …')).toBeInTheDocument()
    await choose(user, within(menu).getByRole('combobox', { name: 'Choose the Year' }), '2020')
    await choose(user, within(menu).getByRole('combobox', { name: 'Choose the Month' }), /June/i)
    await user.click(within(menu).getByRole('button', { name: /June 1st, 2020/ }))
    expect(screen.getByText('Jun 1, 2020 to …')).toBeInTheDocument()
    await user.click(within(menu).getByRole('button', { name: /June 1st, 2020/ }))
    expect(screen.getByText('Jun 1, 2020 to Jun 1, 2020')).toBeInTheDocument()
    await user.click(within(menu).getByRole('button', { name: /June 1st, 2020/ }))
    expect(screen.getAllByText('… to …').length).toBeGreaterThan(0)
    await user.keyboard('{Escape}')

    await user.type(box, 'nothing-like-this')
    expect(screen.queryByRole('grid')).not.toBeInTheDocument()
    expect(screen.getByText('Nothing matches the filter')).toBeInTheDocument()
    const before = asked.count
    await user.click(within(screen.getByText('0 of 5').closest('[role="status"]') as HTMLElement).getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(asked.count).toBe(before + 1))
    await user.click(screen.getAllByRole('button', { name: 'Clear filters' }).at(-1) as HTMLElement)
    expect(names(await screen.findByRole('grid'))).toHaveLength(5)
    expect(grid).not.toBeInTheDocument()
  })

  it('starts without a filter in another folder', async () => {
    mockListings()
    const { user, grid, box } = await renderFiltered()

    await user.type(box, 'assets')
    await user.dblClick(within(grid).getByText('assets'))
    expect(await within(await screen.findByRole('grid')).findByText('inner.txt')).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Filter this folder' })).toHaveValue('')
  })
})
