import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { adminSession, mockSession, mockWorkspaceData } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'

import { Explorer } from './Explorer'
import { listingFreshMs } from './files-key'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** Builds an entry below a folder. */
function entry(folder: string, name: string, type: RemoteEntry['type'] = 'file'): RemoteEntry {
  return { name, path: `${folder}/${name}`, type, size: 10, mode: '-rw-r--r--', modifiedAt: '2026-03-08T07:30:00Z', modifiedPrecision: 'second' }
}

/** The folders of the mock server, by path; a test changes what a folder holds. */
const folders: Record<string, RemoteEntry[]> = {}

/** How many times each folder was asked for. */
const asked: Record<string, number> = {}

/** Answers listings from {@link folders}, counting them, after an optional delay. */
function mockListings(waitMs = 0): void {
  server.use(
    http.get('/api/v1/workspaces/:id/sites/s1/files', async ({ request }) => {
      const path = new URL(request.url).searchParams.get('path') as string
      asked[path] = (asked[path] ?? 0) + 1
      if (waitMs > 0) {
        await delay(waitMs)
      }
      return HttpResponse.json(folders[path] ?? [])
    }),
    http.post('/api/v1/workspaces/:id/sites/s1/files/directories', async ({ request }) => {
      const { path } = (await request.json()) as { path: string }
      return HttpResponse.json(entry('/www', path.slice(5), 'directory'), { status: 201 })
    }),
  )
}

/** An explorer that follows its own navigation, as the route does. */
function Harness() {
  const [path, setPath] = useState('/www')
  return <Explorer workspaceId={workspaceId} siteId="s1" path={path} onNavigate={setPath} />
}

/** Renders the harness and returns what tests need to move around it. */
async function renderHarness() {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
  await renderWithProviders(<Harness />)
  const grid = async () => within(await screen.findByRole('grid'))
  return { user, grid }
}

/**
 * Covers the cache of folder listings in {@link Explorer}.
 *
 * Scenarios:
 * - going back to a folder that was just seen shows it at once and asks the server nothing
 * - once a listing is older than the fresh time, visiting it shows the cached one while the server is asked again in the background, and
 *   the listing changes when the answer arrives
 * - resting the pointer on a folder fetches its listing ahead, so opening it asks nothing more, and files are not fetched
 * - changing something on the connection makes every cached listing of it stale, so the next visit asks again
 * - coming back to the page after the listings went stale reloads the folder in view
 */
describe('listing cache', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockSession(adminSession)
    mockWorkspaceData()
    for (const key of Object.keys(asked)) {
      delete asked[key]
    }
    folders['/www'] = [entry('/www', 'docs', 'directory'), entry('/www', 'index.html')]
    folders['/www/docs'] = [entry('/www/docs', 'guide.md')]
    mockListings()
  })

  afterEach(() => vi.useRealTimers())

  it('shows a folder seen a moment ago at once, without asking the server', async () => {
    const { user, grid } = await renderHarness()

    await user.dblClick((await grid()).getByText('docs'))
    await (await grid()).findByText('guide.md')
    await user.click(screen.getByRole('button', { name: 'Up one level' }))

    expect(await (await grid()).findByText('index.html')).toBeInTheDocument()
    expect(asked).toEqual({ '/www': 1, '/www/docs': 1 })
  })

  it('shows the cached listing while a stale one is refreshed in the background', async () => {
    const { user, grid } = await renderHarness()
    await user.dblClick((await grid()).getByText('docs'))
    await (await grid()).findByText('guide.md')
    await user.click(screen.getByRole('button', { name: 'Up one level' }))
    await (await grid()).findByText('index.html')

    await act(() => vi.advanceTimersByTimeAsync(listingFreshMs + 1000))
    folders['/www/docs'] = [entry('/www/docs', 'guide.md'), entry('/www/docs', 'new.md')]
    server.resetHandlers()
    mockListings(300)
    await user.dblClick((await grid()).getByText('docs'))

    expect(await (await grid()).findByText('guide.md')).toBeInTheDocument()
    expect(screen.queryByText('new.md')).not.toBeInTheDocument()
    expect(screen.queryByText(/Connecting to/)).not.toBeInTheDocument()
    expect(await screen.findByText('new.md')).toBeInTheDocument()
    expect(asked['/www/docs']).toBe(2)
  })

  it('fetches a folder ahead when the pointer rests on it', async () => {
    const { user, grid } = await renderHarness()
    const table = await grid()

    fireEvent.mouseEnter(table.getByText('index.html').closest('[role="row"]') as HTMLElement)
    expect(asked['/www/docs']).toBeUndefined()
    fireEvent.mouseEnter(table.getByText('docs').closest('[role="row"]') as HTMLElement)
    await waitFor(() => expect(asked['/www/docs']).toBe(1))

    await user.dblClick(table.getByText('docs'))
    expect(await (await grid()).findByText('guide.md')).toBeInTheDocument()
    expect(asked['/www/docs']).toBe(1)
  })

  it('makes every cached listing of the connection stale after a change', async () => {
    const { user, grid } = await renderHarness()
    await user.dblClick((await grid()).getByText('docs'))
    await (await grid()).findByText('guide.md')
    await user.click(screen.getByRole('button', { name: 'Up one level' }))
    await (await grid()).findByText('index.html')

    await user.click(screen.getByRole('button', { name: 'New folder' }))
    await user.type(await screen.findByLabelText('Name'), 'fresh')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(asked['/www']).toBe(2))

    await user.dblClick((await grid()).getByText('docs'))
    await waitFor(() => expect(asked['/www/docs']).toBe(2))
  })

  it('reloads the folder in view when the page comes back after the listing went stale', async () => {
    await renderHarness()
    await screen.findByRole('grid')
    expect(asked['/www']).toBe(1)

    await act(() => vi.advanceTimersByTimeAsync(listingFreshMs + 1000))
    const show = (state: 'hidden' | 'visible') => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: state })
      act(() => {
        document.dispatchEvent(new Event('visibilitychange', { bubbles: true }))
      })
    }
    show('hidden')
    show('visible')

    await waitFor(() => expect(asked['/www']).toBe(2))
  })
})
