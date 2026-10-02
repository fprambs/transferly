import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { adminSession, mockSession, mockUnlocking, mockWorkspaceData, personalWorkspace, problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderApp } from '@/test/render-app'
import { renderWithProviders } from '@/test/render'

import { ExportDialog } from './ExportDialog'

/** Workspace the mock server answers for. */
const workspaceId = '0f3c1a52-1d0e-4b8f-8a55-6d2f9b7c4e10'

/** Answers exports with a sealed file and records the passwords sent. */
function mockExport(): string[] {
  const passwords: string[] = []
  server.use(
    http.post('/api/v1/workspaces/:id/exports', async ({ request }) => {
      passwords.push(((await request.json()) as { password: string }).password)
      return new HttpResponse('{"format":"transferly-export"}', { headers: { 'Content-Type': 'application/octet-stream' } })
    }),
  )
  return passwords
}

/** Renders the dialog with a save that records the files, and returns what tests use. */
async function renderDialog() {
  const saved: Array<{ name: string; text: string }> = []
  const onClose = vi.fn()
  const save = (file: Blob, name: string) => void file.text().then((text) => saved.push({ name, text }))
  await renderWithProviders(<ExportDialog open workspaceId={workspaceId} onClose={onClose} save={save} />)
  const dialog = within(await screen.findByRole('dialog'))
  return { user: userEvent.setup(), dialog, saved, onClose }
}

/**
 * Covers {@link ExportDialog} (ADR-0017).
 *
 * Scenarios:
 * - a password shorter than 12 characters and one that does not match its repetition are refused before anything is sent
 * - the export sends the password, saves the file under a name with the day, and says where it went
 * - a locked vault offers to unlock it in place, and other failures are explained
 * - the dialog closes from its button and with Escape, and the sites sidebar opens it
 * - the sidebar offers the export to admins and owners only, since it shows every secret of a team
 */
describe('ExportDialog', () => {
  beforeEach(() => {
    mockSession(adminSession)
    mockWorkspaceData()
  })

  it('checks the password and saves the file', async () => {
    const passwords = mockExport()
    const { user, dialog, saved, onClose } = await renderDialog()

    await user.type(dialog.getByLabelText('File password'), 'short')
    await user.click(dialog.getByRole('button', { name: 'Export' }))
    expect(dialog.getByRole('alert')).toHaveTextContent('at least 12 characters')

    await user.clear(dialog.getByLabelText('File password'))
    await user.type(dialog.getByLabelText('File password'), 'a long export password')
    await user.type(dialog.getByLabelText('Repeat the password'), 'another long password')
    await user.click(dialog.getByRole('button', { name: 'Export' }))
    expect(dialog.getByRole('alert')).toHaveTextContent('The passwords do not match.')
    expect(passwords).toEqual([])

    await user.clear(dialog.getByLabelText('Repeat the password'))
    await user.type(dialog.getByLabelText('Repeat the password'), 'a long export password')
    await user.click(dialog.getByRole('button', { name: 'Export' }))
    expect(await dialog.findByRole('status')).toHaveTextContent(/saved as transferly-sites-\d{4}-\d\d-\d\d\.json/)
    expect(passwords).toEqual(['a long export password'])
    await waitFor(() => expect(saved).toEqual([{ name: expect.stringMatching(/^transferly-sites-/), text: '{"format":"transferly-export"}' }]))

    await user.click(dialog.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalled()
  })

  it('explains a locked vault and other failures', async () => {
    server.use(http.post('/api/v1/workspaces/:id/exports', () => problem(409, 'vault_locked')))
    const { user, dialog } = await renderDialog()
    await user.type(dialog.getByLabelText('File password'), 'a long export password')
    await user.type(dialog.getByLabelText('Repeat the password'), 'a long export password')
    await user.click(dialog.getByRole('button', { name: 'Export' }))
    expect(await dialog.findByText(/Unlock it to export them/)).toBeInTheDocument()
    const unlocking = mockUnlocking()
    await user.click(dialog.getByRole('button', { name: 'Unlock the vault' }))
    const unlock = await screen.findByRole('dialog', { name: 'Unlock the vault' })
    await user.type(within(unlock).getByLabelText('Master password'), 'correct horse battery')
    await user.click(within(unlock).getByRole('button', { name: 'Unlock' }))
    await waitFor(() => expect(unlocking.unlocked()).toBe(true))
    await waitFor(() => expect(dialog.queryByText(/Unlock it to export them/)).not.toBeInTheDocument())

    server.use(http.post('/api/v1/workspaces/:id/exports', () => problem(422, 'weak_export_password')))
    await user.click(dialog.getByRole('button', { name: 'Export' }))
    expect(await dialog.findByRole('alert')).toHaveTextContent('at least 12 characters')
  })

  it('opens from the sites sidebar and saves through the browser', async () => {
    mockExport()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    URL.createObjectURL = vi.fn(() => 'blob:export')
    URL.revokeObjectURL = vi.fn()
    const user = userEvent.setup()
    await renderApp()

    await user.click(await screen.findByRole('button', { name: 'More site actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Export sites' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Export sites' }))
    await user.type(dialog.getByLabelText('File password'), 'a long export password')
    await user.type(dialog.getByLabelText('Repeat the password'), 'a long export password')
    await user.click(dialog.getByRole('button', { name: 'Export' }))
    await dialog.findByRole('status')
    expect(click).toHaveBeenCalled()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:export')

    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('offers the export to admins and owners only', async () => {
    mockWorkspaceData({ workspaces: [{ ...personalWorkspace, kind: 'team', name: 'Acme', role: 'editor' }] })
    await renderApp()
    await userEvent.setup().click(await screen.findByRole('button', { name: 'More site actions' }))
    expect(await screen.findByRole('menuitem', { name: 'Import sites' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: 'Export sites' })).not.toBeInTheDocument()
  })
})
