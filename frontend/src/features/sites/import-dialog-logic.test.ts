import { describe, expect, it } from 'vitest'

import { filezillaMasterRejected, filezillaUsesCrypt } from './import-dialog-logic'

/**
 * Covers {@link filezillaMasterRejected} and {@link filezillaUsesCrypt}.
 *
 * Scenarios:
 * - a preview is not taken as a refused master password when passwords are not imported, is when every site password was skipped, and
 *   is not when at least one password was read, nor when no site has a password at all
 * - crypt passwords are told by their encoding attribute, in double or single quotes
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

  it('is false when no site has a password', () => {
    expect(
      filezillaMasterRejected(
        { dryRun: true, items: [{ name: 'A', path: '', status: 'skipped', passwordStored: false, passwordSkipped: true }, { name: 'B', path: '', status: 'would_create', passwordStored: false, passwordSkipped: false }] },
        true,
      ),
    ).toBe(false)
  })

  it('tells crypt passwords by their encoding', () => {
    expect(filezillaUsesCrypt('<Pass encoding="crypt">x</Pass>')).toBe(true)
    expect(filezillaUsesCrypt("<Pass encoding='crypt'>x</Pass>")).toBe(true)
    expect(filezillaUsesCrypt('<Pass encoding="base64">x</Pass>')).toBe(false)
  })
})
