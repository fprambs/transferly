import { Blob as NodeBlob, File as NodeFile } from 'node:buffer'

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { adminSession, mockSession, mockUnlocking, mockWorkspaceData, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { choose as chooseOption } from '@/test/select'
import { renderApp } from '@/test/render-app'
import { renderWithProviders } from '@/test/render'

import { ImportDialog } from './ImportDialog'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** What the mock server saw of an import. */
interface Seen {
  /** Query of the request. */
  query: string
  /** Media type of the body. */
  type: string | null
  /** The file, as text. */
  body: string
}

/** Answers imports with a result made of the given items and records the requests. */
function mockImport(items: Array<Record<string, unknown>>, seen: Seen[] = []): Seen[] {
  server.use(
    http.post('/api/v1/workspaces/:id/imports/filezilla', async ({ request }) => {
      const url = new URL(request.url)
      seen.push({ query: url.search, type: request.headers.get('Content-Type'), body: await request.text() })
      return HttpResponse.json({ dryRun: url.searchParams.get('dryRun') === 'true', items })
    }),
  )
  return seen
}

/** A site manager whose passwords FileZilla sealed with a master password. */
const cryptManager = '<FileZilla3><Servers><Server><Pass encoding="crypt" pubkey="k">p</Pass></Server></Servers></FileZilla3>'

/** Answers FileZilla imports opening the passwords only with the master password "right", and records the master passwords sent. */
function mockCryptImport(): Array<string | null> {
  const masters: Array<string | null> = []
  server.use(
    http.post('/api/v1/workspaces/:id/imports/filezilla', ({ request }) => {
      const master = request.headers.get('X-FileZilla-Master-Password')
      masters.push(master)
      const right = master === 'right'
      const dryRun = new URL(request.url).searchParams.get('dryRun') === 'true'
      return HttpResponse.json({ dryRun, items: dryRun ? [{ name: 'Prod', path: '', status: 'would_create', passwordStored: right, passwordSkipped: !right }] : [] })
    }),
  )
  return masters
}

/** The items of a typical preview. */
const items = [
  { name: 'Prod', path: 'Work/Deep', status: 'would_create', passwordStored: true, passwordSkipped: false },
  { name: 'Staging', path: '', status: 'would_create', passwordStored: false, passwordSkipped: true },
  { name: 'Web', path: '', status: 'skipped', reason: 'unsupported_protocol', passwordStored: false, passwordSkipped: false },
  { name: 'Odd', path: '', status: 'failed', reason: 'something_new', passwordStored: false, passwordSkipped: false },
]

/** Renders the dialog and returns what tests use. */
async function renderDialog(language: 'en' | 'es' = 'en') {
  vi.stubGlobal('Blob', NodeBlob)
  const onClose = vi.fn()
  const user = userEvent.setup()
  await renderWithProviders(<ImportDialog open workspaceId={workspaceId} onClose={onClose} />, language)
  const dialog = await screen.findByRole('dialog')
  return { user, onClose, dialog: within(dialog) }
}

/** Closes every dialog rendered so far and renders a new one. */
async function renderDialogAgain() {
  cleanup()
  return renderDialog()
}

/** Chooses a file in the dialog and waits until it was read and its kind is shown. */
async function choose(dialog: ReturnType<typeof within>, name = 'sitemanager.xml', content = '<FileZilla3/>'): Promise<void> {
  fireEvent.change(dialog.getByLabelText('File to import'), { target: { files: [new NodeFile([content], name)] } })
  await dialog.findByText(content.includes('transferly-export') ? 'Transferly export, encrypted' : 'FileZilla site manager')
}

/**
 * Covers {@link ImportDialog}.
 *
 * Scenarios:
 * - nothing can be previewed until a file is chosen; import runs a preview first when needed
 * - the preview sends the file as XML with the options, lists the sites with their folder, status, reason and password notes, and counts them
 * - reasons the interface knows are explained and others are shown as they are
 * - importing sends the request without the dry run, shows what was created and offers to close
 * - a rejected file or a locked vault is explained, and choosing another file clears the preview
 * - the dialog can be cancelled, and it follows the interface language
 * - the sites sidebar opens it from its import button
 * - a file exported by Transferly is told apart by its content, needs its password, and goes to its own import with the secrets option
 *   (ADR-0017); text that is not JSON, or JSON of another kind, is taken for a FileZilla file
 * - a FileZilla file with crypt passwords asks for its master password, checks it a moment after it is typed and says whether it opens
 *   the passwords, refuses to import with a wrong one, and stops checking when passwords are not imported or the check fails
 * - a file can be dropped on the dialog and removed, and closing the dialog drops a pending check
 */
describe('ImportDialog', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  afterEach(() => vi.unstubAllGlobals())

  it('previews before it imports', async () => {
    const seen = mockImport(items)
    const { user, dialog } = await renderDialog()

    expect(dialog.getByRole('button', { name: 'Preview' })).toBeDisabled()
    expect(dialog.getByRole('button', { name: 'Import' })).toBeDisabled()
    await choose(dialog)
    expect(dialog.getByRole('button', { name: 'Import' })).toBeEnabled()
    await user.click(dialog.getByRole('checkbox'))
    await chooseOption(user, dialog.getByRole('combobox'), 'Keep both, renaming the new one')
    await user.click(dialog.getByRole('button', { name: 'Preview' }))

    expect(await dialog.findByText('2 sites will be created, 2 skipped.')).toBeInTheDocument()
    expect(seen).toEqual([{ query: '?dryRun=true&passwords=true&onConflict=rename', type: 'application/xml', body: '<FileZilla3/>' }])
    const rows = dialog.getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('Prod')
    expect(rows[0]).toHaveTextContent('Work/Deep')
    expect(rows[0]).toHaveTextContent('Will be created')
    expect(rows[0]).toHaveTextContent('password saved')
    expect(rows[1]).toHaveTextContent('password not saved')
    expect(rows[2]).toHaveTextContent('Skipped')
    expect(rows[2]).toHaveTextContent('Protocol not supported')
    expect(rows[3]).toHaveTextContent('Failed')
    expect(rows[3]).toHaveTextContent('something_new')
    expect(dialog.getByRole('button', { name: 'Import' })).toBeEnabled()
  })

  it('imports after the preview', async () => {
    const seen = mockImport(items)
    const { user, onClose, dialog } = await renderDialog()
    await choose(dialog)
    await user.click(dialog.getByRole('button', { name: 'Import' }))

    expect(await dialog.findByText('0 sites created, 1 skipped, 1 failed.')).toBeInTheDocument()
    expect(seen[0].query).toBe('?dryRun=true&passwords=false&onConflict=skip')
    expect(seen[1].query).toBe('?dryRun=false&passwords=false&onConflict=skip')
    expect(dialog.queryByRole('button', { name: 'Preview' })).not.toBeInTheDocument()
    await user.click(dialog.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalled()
  })

  it('explains a rejected file and a locked vault, and clears the preview with another file', async () => {
    mockImport(items)
    const { user, dialog } = await renderDialog()
    await choose(dialog)
    await user.click(dialog.getByRole('button', { name: 'Preview' }))
    await dialog.findByText(/will be created/)

    await choose(dialog, 'other.xml')
    expect(dialog.queryByText(/will be created/)).not.toBeInTheDocument()
    server.use(http.post('/api/v1/workspaces/:id/imports/filezilla', () => problem(422, 'invalid_import_file')))
    await user.click(dialog.getByRole('button', { name: 'Preview' }))
    expect(await dialog.findByRole('alert')).toHaveTextContent('This is not a FileZilla site manager file.')

    server.use(http.post('/api/v1/workspaces/:id/imports/filezilla', () => problem(409, 'vault_locked')))
    await user.click(dialog.getByRole('button', { name: 'Preview' }))
    await waitFor(() => expect(dialog.getByRole('status')).toHaveTextContent('Your vault is locked'))

    const unlocking = mockUnlocking()
    await user.click(within(dialog.getByRole('status')).getByRole('button', { name: 'Unlock the vault' }))
    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    await user.type(within(unlock).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(unlock).getByRole('button', { name: 'Unlock' }))
    await waitFor(() => expect(unlocking.unlocked()).toBe(true))
    await waitFor(() => expect(dialog.queryByRole('status')).not.toBeInTheDocument())

    fireEvent.change(dialog.getByLabelText('File to import'), { target: { files: [] } })
    expect(dialog.getByRole('button', { name: 'Preview' })).toBeDisabled()
    expect(dialog.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('imports a Transferly export with its password', async () => {
    const seen: Array<{ query: string; body: unknown }> = []
    server.use(
      http.post('/api/v1/workspaces/:id/imports/transferly', async ({ request }) => {
        const url = new URL(request.url)
        seen.push({ query: url.search, body: await request.json() })
        return HttpResponse.json({ dryRun: true, items: items.slice(0, 1) })
      }),
    )
    const { user, dialog } = await renderDialog()
    const exported = JSON.stringify({ format: 'transferly-export', version: 1 })
    await choose(dialog, 'transferly-sites.json', exported)

    expect(dialog.getByRole('button', { name: 'Preview' })).toBeDisabled()
    await user.type(dialog.getByLabelText('File password'), 'a long export password')
    await user.click(dialog.getByRole('checkbox', { name: /Import the passwords and keys/ }))
    await user.click(dialog.getByRole('button', { name: 'Preview' }))

    expect(await dialog.findByText('1 sites will be created, 0 skipped.')).toBeInTheDocument()
    expect(seen).toEqual([{ query: '?dryRun=true&secrets=true&onConflict=skip', body: { password: 'a long export password', file: exported } }])

    await choose(dialog, 'other.json', '{"format":"other"}')
    expect(dialog.queryByLabelText('File password')).not.toBeInTheDocument()
  })

  it('checks the FileZilla master password and refuses to import with a wrong one', async () => {
    const masters = mockCryptImport()
    const { user, dialog } = await renderDialog()
    await choose(dialog, 'sitemanager.xml', cryptManager)
    expect(dialog.getByRole('checkbox', { name: /Import the saved passwords/ })).toBeChecked()
    expect(dialog.getByRole('button', { name: 'Import' })).toBeDisabled()

    await user.type(dialog.getByLabelText('FileZilla master password'), 'wrong')
    expect(dialog.getByRole('status')).toHaveTextContent('Checking the master password…')
    expect(await dialog.findByRole('alert', {}, { timeout: 3000 })).toHaveTextContent('This master password does not open the site passwords in the file.')
    expect(masters).toEqual(['wrong'])
    expect(dialog.getByRole('button', { name: 'Import' })).toBeDisabled()

    await chooseOption(user, dialog.getByRole('combobox'), 'Keep both, renaming the new one')
    await user.clear(dialog.getByLabelText('FileZilla master password'))
    await user.type(dialog.getByLabelText('FileZilla master password'), 'right')
    expect(await dialog.findByText('Master password opens the site passwords in this file.', {}, { timeout: 3000 })).toBeInTheDocument()
    await user.click(dialog.getByRole('button', { name: 'Import' }))
    expect(await dialog.findByText('0 sites created, 0 skipped, 0 failed.')).toBeInTheDocument()
    expect(masters.at(-1)).toBe('right')
  })

  it('stops checking when passwords are not imported, and blocks a preview that rejects the master password', async () => {
    const masters = mockCryptImport()
    const { user, dialog } = await renderDialog()
    await choose(dialog, 'sitemanager.xml', cryptManager)
    await user.click(dialog.getByRole('checkbox', { name: /Import the saved passwords/ }))
    await user.type(dialog.getByLabelText('FileZilla master password'), 'wrong')
    expect(dialog.queryByRole('status')).not.toBeInTheDocument()
    await user.click(dialog.getByRole('checkbox', { name: /Import the saved passwords/ }))
    await user.click(dialog.getByRole('button', { name: 'Import' }))
    expect(await dialog.findByRole('alert')).toHaveTextContent('This master password does not open the site passwords in the file.')
    expect(masters.filter((master) => master === 'wrong').length).toBeGreaterThan(0)
    expect(dialog.queryByText(/sites created/)).not.toBeInTheDocument()

    server.use(http.post('/api/v1/workspaces/:id/imports/filezilla', () => problem(500, 'internal_error')))
    await user.type(dialog.getByLabelText('FileZilla master password'), '!')
    await waitFor(() => expect(dialog.queryByText('Checking the master password…')).not.toBeInTheDocument(), { timeout: 3000 })
    expect(dialog.queryByText('This master password does not open the site passwords in the file.')).not.toBeInTheDocument()
  })

  it('reuses a preview when importing, and stops when the preview fails', async () => {
    const seen = mockImport(items)
    const { user, dialog } = await renderDialog()
    const clicked = vi.spyOn(HTMLInputElement.prototype, 'click').mockImplementation(() => undefined)
    await user.click(dialog.getByRole('button', { name: 'Choose a file' }))
    expect(clicked).toHaveBeenCalled()
    await choose(dialog)
    await user.click(dialog.getByRole('button', { name: 'Preview' }))
    await dialog.findByText(/will be created/)
    await user.click(dialog.getByRole('button', { name: 'Import' }))
    await dialog.findByText(/sites created/)
    expect(seen.map((request) => request.query.startsWith('?dryRun=true'))).toEqual([true, false])

    const failing = await renderDialogAgain()
    server.use(http.post('/api/v1/workspaces/:id/imports/filezilla', () => problem(422, 'invalid_import_file')))
    await choose(failing.dialog)
    await failing.user.click(failing.dialog.getByRole('button', { name: 'Import' }))
    expect(await failing.dialog.findByRole('alert')).toHaveTextContent('This is not a FileZilla site manager file.')
    expect(failing.dialog.queryByText(/sites created/)).not.toBeInTheDocument()
  })

  it('takes a dropped file, removes it, and drops a pending check when closed', async () => {
    mockCryptImport()
    const { user, dialog } = await renderDialog()
    const zone = dialog.getByText('or drop it here').parentElement as HTMLElement
    fireEvent.dragOver(zone)
    expect(zone.className).toContain('border-accent')
    fireEvent.dragLeave(zone)
    expect(zone.className).not.toContain('border-accent')
    fireEvent.drop(zone, { dataTransfer: { files: [new NodeFile([cryptManager], 'dropped.xml')] } })
    expect(await dialog.findByText('dropped.xml')).toBeInTheDocument()
    await user.click(dialog.getByRole('button', { name: 'Remove the file' }))
    expect(dialog.queryByText('dropped.xml')).not.toBeInTheDocument()
    expect(dialog.getByRole('button', { name: 'Choose a file' })).toBeInTheDocument()

    await choose(dialog, 'sitemanager.xml', cryptManager)
    await user.type(dialog.getByLabelText('FileZilla master password'), 'x')
    cleanup()
  })

  it('can be cancelled and speaks Spanish', async () => {
    const { user, onClose, dialog } = await renderDialog('es')

    expect(dialog.getByRole('heading', { name: 'Importar sitios' })).toBeInTheDocument()
    expect(dialog.getByRole('button', { name: 'Vista previa' })).toBeDisabled()
    await user.click(dialog.getByRole('button', { name: 'Cancelar' }))
    expect(onClose).toHaveBeenCalled()
  })

  it('opens from the sites sidebar', async () => {
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'More site actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Import sites' }))

    expect(await screen.findByRole('dialog', { name: 'Import sites' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
