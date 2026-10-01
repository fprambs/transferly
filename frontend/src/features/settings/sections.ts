import { useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback } from 'react'

/** The sections of the settings dialog, in the order its side bar lists them. */
export const settingsSections = ['general', 'security', 'servers', 'members', 'audit', 'admin'] as const

/** A section of the settings dialog. */
export type SettingsSection = (typeof settingsSections)[number]

/** What {@link useSettingsDialog} gives: the open section, if any, and how to open another one or close the dialog. */
export interface SettingsDialogControl {
  /** The section in view, or `undefined` while the dialog is closed. */
  section: SettingsSection | undefined
  /** Opens the dialog on a section, or moves it there. */
  open: (section: SettingsSection) => void
  /** Closes the dialog and leaves the page under it as it was. */
  close: () => void
}

/**
 * Reads and changes the section of the settings dialog, kept in the `settings` search parameter of the address: the page under the dialog
 * stays mounted, so its connections, tabs and transfers carry on, and the back button closes the dialog.
 */
export function useSettingsDialog(): SettingsDialogControl {
  const navigate = useNavigate()
  const { settings } = useSearch({ strict: false }) as { settings?: SettingsSection }
  const go = useCallback(
    (section: SettingsSection | undefined) => void navigate({ to: '.', search: (previous: Record<string, unknown>) => ({ ...previous, settings: section }) } as never),
    [navigate],
  )
  return { section: settings, open: go, close: useCallback(() => go(undefined), [go]) }
}
