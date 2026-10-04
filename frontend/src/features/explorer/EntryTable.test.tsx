import { fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { renderWithI18n } from '@/test/render'

import { EntryTable } from './EntryTable'
import { Inspector } from './Inspector'

/** A single file entry, with no folders around it. */
function file(): RemoteEntry {
  return { name: 'notes.txt', path: '/notes.txt', type: 'file', size: 11, mode: '-rw-r--r--', modifiedAt: '2026-01-01T00:00:00Z', modifiedPrecision: 'second' }
}

/**
 * Covers {@link EntryTable} in isolation, for the prop combinations {@link Explorer} and {@link LocalExplorer} never
 * both exercise together.
 *
 * Scenarios:
 * - a file with no context menu action wired shows no context menu at all
 * - a file with only `onEdit` wired shows a context menu with just that action
 * - a file opened with a double click or Enter goes to `onOpenFile` when it is wired
 * - a folder the server knows no time of, as an object store prefix, shows a dash for its date in the table and in the details
 */
describe('EntryTable', () => {
  it('shows no context menu for a file with no action wired', async () => {
    await renderWithI18n(
      <EntryTable entries={[file()]} locale="en" timeZone="UTC" onOpen={vi.fn()} onUp={vi.fn()} showPermissions selectedPath={null} onSelect={vi.fn()} onDragEntry={vi.fn()} />,
    )
    fireEvent.contextMenu(screen.getByText('notes.txt'))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('offers to edit a file from the context menu when onEdit is the only action wired', async () => {
    const onEdit = vi.fn()
    const user = userEvent.setup()
    await renderWithI18n(
      <EntryTable entries={[file()]} locale="en" timeZone="UTC" onOpen={vi.fn()} onUp={vi.fn()} showPermissions selectedPath={null} onSelect={vi.fn()} onDragEntry={vi.fn()} onEdit={onEdit} />,
    )
    fireEvent.contextMenu(screen.getByText('notes.txt'))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }))
    expect(onEdit).toHaveBeenCalledWith(file())
  })

  it('hands a file opened with a double click or Enter to onOpenFile instead of onOpen', async () => {
    const onOpen = vi.fn()
    const onOpenFile = vi.fn()
    const user = userEvent.setup()
    await renderWithI18n(
      <EntryTable entries={[file()]} locale="en" timeZone="UTC" onOpen={onOpen} onOpenFile={onOpenFile} onUp={vi.fn()} showPermissions selectedPath={null} onSelect={vi.fn()} onDragEntry={vi.fn()} />,
    )
    await user.dblClick(screen.getByText('notes.txt'))
    screen.getByRole('grid').focus()
    await user.keyboard('{Enter}')
    expect(onOpenFile).toHaveBeenCalledTimes(2)
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('shows a dash for a folder the server knows no time of', async () => {
    const prefix: RemoteEntry = { name: 'photos', path: '/photos', type: 'directory', size: 0, mode: 'drwxr-xr-x', modifiedAt: '0001-01-01T00:00:00Z', modifiedPrecision: 'second' }
    await renderWithI18n(
      <>
        <EntryTable entries={[prefix]} locale="en" timeZone="UTC" onOpen={vi.fn()} onUp={vi.fn()} showPermissions={false} selectedPath={null} onSelect={vi.fn()} onDragEntry={vi.fn()} />
        <Inspector
          entry={prefix}
          showPermissions={false}
          locale="en"
          timeZone="UTC"
          onDownload={vi.fn()}
          onDownloadEach={vi.fn()}
          onChecksum={vi.fn()}
          onRename={vi.fn()}
          onDelete={vi.fn()}
        />
      </>,
    )
    expect(screen.getByRole('row', { name: /photos/ })).not.toHaveTextContent(/0001|Jan 1, 1/)
    expect(screen.getByText('Modified', { selector: 'dt' }).nextElementSibling).toHaveTextContent('—')
  })
})
