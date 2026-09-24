import { describe, expect, it } from 'vitest'

import { findPhysicalDirectionUtilities } from './rtl-rules.ts'

/**
 * Covers {@link findPhysicalDirectionUtilities}.
 *
 * Scenarios:
 * - physical margin, padding, position, text, float, border and radius utilities are reported
 * - logical utilities are accepted
 * - lines that are not class definitions are ignored
 * - the reported line and utility identify the problem
 */
describe('findPhysicalDirectionUtilities', () => {
  it.each(['ml-2', 'mr-4', 'pl-3', 'pr-3', 'left-0', 'right-1', 'text-left', 'text-right', 'float-left', 'border-l', 'border-r-2', 'rounded-l-md', 'rounded-tr'])(
    'reports %s',
    (utility) => {
      expect(findPhysicalDirectionUtilities(`<div className="flex ${utility}" />`)).toHaveLength(1)
    },
  )

  it.each(['ms-2', 'me-4', 'ps-3', 'pe-3', 'start-0', 'end-1', 'text-start', 'text-end', 'border-s', 'rounded-s-md', 'mt-2', 'px-4', 'text-left-aligned-copy'])(
    'accepts %s',
    (utility) => {
      expect(findPhysicalDirectionUtilities(`<div className="flex ${utility}" />`)).toEqual([])
    },
  )

  it('ignores lines that are not class definitions', () => {
    expect(findPhysicalDirectionUtilities('const label = "ml-2 is a class"')).toEqual([])
  })

  it('reports the line and the utility', () => {
    const text = ['const a = 1', '<p className="text-sm ml-2">x</p>'].join('\n')

    expect(findPhysicalDirectionUtilities(text)).toEqual([{ line: 2, utility: 'ml-2' }])
  })

  it('checks continuation lines of class variants', () => {
    expect(findPhysicalDirectionUtilities('  "hover:mr-2 flex",')).toHaveLength(1)
  })
})
