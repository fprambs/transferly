import { screen, waitFor } from '@testing-library/react'

/** Whether a dialog is another than the settings dialog, which stays open under the dialogs its sections open. */
function overSettings(name: string): boolean {
  return name !== 'Settings' && name !== 'Configuración'
}

/** The dialog open over the settings dialog, or `null` when there is none. */
export function queryInnerDialog(): HTMLElement | null {
  return screen.queryAllByRole('dialog', { name: overSettings }).at(-1) ?? null
}

/** Waits for a dialog to open over the settings dialog and returns it. */
export function findInnerDialog(): Promise<HTMLElement> {
  return waitFor(() => {
    const dialog = queryInnerDialog()
    if (dialog === null) {
      throw new Error('no dialog opened over the settings')
    }
    return dialog
  })
}
