import { fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { RemoteEntry } from '@/lib/api'
import { renderWithI18n } from '@/test/render'

import { EntryTable } from './EntryTable'

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
})
