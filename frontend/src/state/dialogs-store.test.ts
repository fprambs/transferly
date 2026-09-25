import { describe, expect, it } from 'vitest'

import { useDialogsStore } from './dialogs-store'

/**
 * Covers {@link useDialogsStore}.
 *
 * Scenarios:
 * - the quick connect dialog starts closed and can be opened and closed
 */
describe('dialogs store', () => {
  it('opens and closes the quick connect dialog', () => {
    expect(useDialogsStore.getState().quickConnectOpen).toBe(false)

    useDialogsStore.getState().setQuickConnectOpen(true)
    expect(useDialogsStore.getState().quickConnectOpen).toBe(true)

    useDialogsStore.getState().setQuickConnectOpen(false)
    expect(useDialogsStore.getState().quickConnectOpen).toBe(false)
  })
})
