import { act, cleanup, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { createApi } from '@/lib/api'
import type { RemoteEntry } from '@/lib/api'
import { HttpTransport } from '@/lib/transport'
import { useUiStore } from '@/state/ui-store'
import { server } from '@/test/msw'
import { renderWithI18n } from '@/test/render'

import { loadLanguageFor } from './editor-language'
import { editorErrorKeyOf } from './errors'
import { maxEditableBytes, RemoteEditor } from './RemoteEditor'

/** Workspace and site the mock server answers for. */
const workspaceId = 'w1'
const siteId = 's1'

/** The file being edited. */
function entry(overrides: Partial<RemoteEntry> = {}): RemoteEntry {
  return { name: 'notes.txt', path: '/notes.txt', type: 'file', size: 11, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second', ...overrides }
}

/** Answers the content and the stat of the file; stat answers content's stamp unless given its own. */
function mockFile(content: string, stat?: Partial<RemoteEntry>): void {
  const stamp = entry({ size: content.length, ...stat })
  server.use(
    http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/content`, () => HttpResponse.text(content)),
    http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/entry`, () => HttpResponse.json(stamp)),
  )
}

/** Finds the editable text area CodeMirror mounts inside the dialog. */
function editorContent(dialog: HTMLElement): HTMLElement {
  return within(dialog).getByRole('textbox')
}

/**
 * Pastes text at the end of the editor's content, the way CodeMirror can be driven from a test without simulating
 * real typing. The cursor starts at the document's beginning, so it is moved to the end first with the End key,
 * which CodeMirror's default keymap handles regardless of where the caret already sits on the line.
 */
async function paste(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement, text: string): Promise<void> {
  const content = editorContent(dialog)
  await user.click(content)
  await user.keyboard('{End}')
  const event = new Event('paste', { bubbles: true, cancelable: true }) as ClipboardEvent
  Object.defineProperty(event, 'clipboardData', { value: { getData: () => text } })
  content.dispatchEvent(event)
}

/**
 * Covers {@link RemoteEditor}.
 *
 * Scenarios:
 * - the file's content loads into the editor and closing it right away needs no confirmation
 * - a file over the size limit is never fetched and shows its own message instead
 * - saving uploads the edited content with mode=overwrite and updates the record it goes by afterwards
 * - editing and then closing asks to discard, and keeps the dialog open until confirmed
 * - a save whose file changed since it was opened offers to reload the other version or overwrite it anyway
 * - a failure while loading or saving is explained
 */
describe('RemoteEditor', () => {
  const api = createApi(new HttpTransport())

  it('loads the content and closes without asking when nothing changed', async () => {
    mockFile('hello there')
    const user = userEvent.setup()
    const onClose = vi.fn()
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={onClose} onSaved={vi.fn()} />)

    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('hello there'))

    await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('never fetches a file over the size limit, and explains why', async () => {
    let fetched = false
    server.use(http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/content`, () => ((fetched = true), HttpResponse.text('x'))))
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry({ size: maxEditableBytes + 1 })} onClose={vi.fn()} onSaved={vi.fn()} />)

    expect(await screen.findByText(/too large to edit here/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(fetched).toBe(false)
  })

  it('saves the edited content with mode=overwrite and updates the record it goes by', async () => {
    mockFile('hello there')
    let seen: { mode: string | null; body: string } | undefined
    server.use(
      http.put(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/content`, async ({ request }) => {
        seen = { mode: new URL(request.url).searchParams.get('mode'), body: await request.text() }
        return HttpResponse.json(entry({ size: seen.body.length, modifiedAt: '2026-01-01T00:01:00Z' }), { status: 201 })
      }),
    )
    const user = userEvent.setup()
    const onSaved = vi.fn()
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={onSaved} />)

    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('hello there'))
    await paste(user, dialog, '!!!')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(seen).toBeDefined())
    expect(seen).toMatchObject({ mode: 'overwrite', body: 'hello there!!!' })
    expect(onSaved).toHaveBeenCalledTimes(1)
  })

  it('asks to discard unsaved changes before closing, and keeps editing until confirmed', async () => {
    mockFile('hello there')
    const user = userEvent.setup()
    const onClose = vi.fn()
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={onClose} onSaved={vi.fn()} />)

    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('hello there'))
    await paste(user, dialog, '!')

    await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    expect(await within(dialog).findByText('You have unsaved changes. Close without saving them?')).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()

    await user.click(within(dialog).getByRole('button', { name: 'Keep editing' }))
    expect(screen.queryByText('You have unsaved changes. Close without saving them?')).not.toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    await user.click(await within(dialog).findByRole('button', { name: 'Discard changes' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('offers to reload the other version when the file changed since it was opened', async () => {
    mockFile('hello there')
    const user = userEvent.setup()
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={vi.fn()} />)

    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('hello there'))

    server.use(http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/entry`, () => HttpResponse.json(entry({ size: 999, modifiedAt: '2026-06-01T00:00:00Z' }))))
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByText('This file changed since you opened it')).toBeInTheDocument()

    mockFile('someone else wrote this')
    await user.click(within(dialog).getByRole('button', { name: 'Reload their version' }))
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('someone else wrote this'))
    expect(screen.queryByText('This file changed since you opened it')).not.toBeInTheDocument()
  })

  it('offers to overwrite the other version anyway when the file changed since it was opened', async () => {
    mockFile('hello there')
    const user = userEvent.setup()
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={vi.fn()} />)

    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('hello there'))

    server.use(http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/entry`, () => HttpResponse.json(entry({ size: 999, modifiedAt: '2026-06-01T00:00:00Z' }))))
    let overwritten = false
    server.use(
      http.put(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/content`, async ({ request }) => {
        overwritten = true
        return HttpResponse.json(entry({ size: (await request.text()).length }), { status: 201 })
      }),
    )
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByText('This file changed since you opened it')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Overwrite anyway' }))
    await waitFor(() => expect(overwritten).toBe(true))
    expect(screen.queryByText('This file changed since you opened it')).not.toBeInTheDocument()
  })

  it('explains a failure to load the file', async () => {
    server.use(
      http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/content`, () =>
        HttpResponse.json({ type: 'about:blank', title: 'x', status: 404, code: 'path_not_found' }, { status: 404, headers: { 'Content-Type': 'application/problem+json' } }),
      ),
      http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/entry`, () => HttpResponse.json(entry())),
    )
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={vi.fn()} />)
    expect(await screen.findByText('This file no longer exists.')).toBeInTheDocument()
  })

  it('explains a failure to save the file', async () => {
    mockFile('hello there')
    const user = userEvent.setup()
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={vi.fn()} />)
    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('hello there'))
    server.use(
      http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/entry`, () =>
        HttpResponse.json({ type: 'about:blank', title: 'x', status: 502, code: 'remote_unavailable' }, { status: 502, headers: { 'Content-Type': 'application/problem+json' } }),
      ),
    )
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByText('The server did not answer.')).toBeInTheDocument()
  })

  it('opens with the light CodeMirror theme when the interface is light, and follows a theme change', async () => {
    useUiStore.setState({ themeId: 'sentinel-light', followSystem: false })
    mockFile('hello there')
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={vi.fn()} />)
    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(editorContent(dialog)).toHaveTextContent('hello there'))

    act(() => useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false }))
    expect(within(dialog).getByRole('textbox')).toBeInTheDocument()

    act(() => useUiStore.setState({ themeId: 'sentinel-light', followSystem: false }))
    expect(within(dialog).getByRole('textbox')).toBeInTheDocument()

    useUiStore.setState({ themeId: 'sentinel-dark', followSystem: false })
  })

  it('ignores the loaded file if it is closed before loading finishes', async () => {
    mockFile('hello there')
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={vi.fn()} />)
    cleanup()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('ignores a failure to load if it is closed before loading finishes', async () => {
    server.use(
      http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/content`, () =>
        HttpResponse.json({ type: 'about:blank', title: 'x', status: 404, code: 'path_not_found' }, { status: 404, headers: { 'Content-Type': 'application/problem+json' } }),
      ),
      http.get(`/api/v1/workspaces/${workspaceId}/sites/${siteId}/files/entry`, () => HttpResponse.json(entry())),
    )
    await renderWithI18n(<RemoteEditor api={api} workspaceId={workspaceId} siteId={siteId} entry={entry()} onClose={vi.fn()} onSaved={vi.fn()} />)
    cleanup()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('maps a failure that is not an API error to the generic message', () => {
    expect(editorErrorKeyOf(new Error('boom'))).toBe('unknown')
  })

  it('loads the language support of a file recognized by its name', async () => {
    expect(await loadLanguageFor('script.js')).not.toBeNull()
    expect(await loadLanguageFor('README')).toBeNull()
  })
})
