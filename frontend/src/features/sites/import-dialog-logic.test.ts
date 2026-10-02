import { describe, expect, it } from 'vitest'

import { filezillaMasterRejected } from './import-dialog-logic'

/**
 * Covers {@link filezillaMasterRejected}.
 *
 * Scenarios:
 * - a preview is not taken as a refused master password when passwords are not imported, is when every site password was skipped, and
 *   is not when at least one password was read
 */
describe('filezillaMasterRejected', () => {
  it('is false when passwords are not imported', () => {
    expect(
      filezillaMasterRejected(
        { dryRun: true, items: [{ name: 'A', path: '', status: 'would_create', passwordStored: false, passwordSkipped: true }] },
        false,
      ),
    ).toBe(false)
  })

  it('is true when every site password was skipped', () => {
    expect(
      filezillaMasterRejected(
        {
          dryRun: true,
          items: [
            { name: 'A', path: '', status: 'would_create', passwordStored: false, passwordSkipped: true },
            { name: 'B', path: '', status: 'would_create', passwordStored: false, passwordSkipped: true },
          ],
        },
        true,
      ),
    ).toBe(true)
  })

  it('is false when at least one password was read', () => {
    expect(
      filezillaMasterRejected(
        {
          dryRun: true,
          items: [
            { name: 'A', path: '', status: 'would_create', passwordStored: true, passwordSkipped: false },
            { name: 'B', path: '', status: 'would_create', passwordStored: false, passwordSkipped: true },
          ],
        },
        true,
      ),
    ).toBe(false)
  })
})
