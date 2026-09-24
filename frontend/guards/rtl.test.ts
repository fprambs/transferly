import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

import { findPhysicalDirectionUtilities } from './rtl-rules.ts'

const sourceRoot = path.resolve(import.meta.dirname, '../src')

/** Lists the component sources under a directory, skipping tests and declaration files. */
function listComponents(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      return listComponents(full)
    }
    const isSource = /\.tsx$/.test(entry.name) && !/\.test\.tsx$/.test(entry.name)
    return isSource ? [full] : []
  })
}

/**
 * Right-to-left readiness of the interface sources.
 *
 * Scenarios:
 * - no component uses a Tailwind utility tied to the physical left or right side
 */
describe('right-to-left readiness', () => {
  it('uses only logical direction utilities', () => {
    const found = listComponents(sourceRoot).flatMap((file) =>
      findPhysicalDirectionUtilities(readFileSync(file, 'utf8')).map(
        (violation) => `${path.relative(sourceRoot, file)}:${violation.line}: ${violation.utility}`,
      ),
    )

    expect(found).toEqual([])
  })
})
