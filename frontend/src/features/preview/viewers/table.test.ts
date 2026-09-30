import { describe, expect, it } from 'vitest'

import { compareCells, parseTable } from './table'

/**
 * Covers {@link parseTable} and {@link compareCells} (ADR-0018).
 *
 * Scenarios:
 * - the delimiter is guessed (comma, semicolon), TSV files use the tab, quoted cells keep their delimiter, and empty lines are left out
 * - the first row is the header, or columns are numbered without one; short rows are padded
 * - numbers sort as numbers and text in the natural order of the language
 */
describe('tables', () => {
  it('guesses the delimiter and pads rows', () => {
    expect(parseTable('a.csv', 'a,b,c\n1,"2,5"\n\n3,4,5\n', true)).toEqual({ columns: ['a', 'b', 'c'], rows: [['1', '2,5', ''], ['3', '4', '5']], delimiter: ',' })
    expect(parseTable('b.csv', 'x;y\n1;2\n', true).delimiter).toBe(';')
    expect(parseTable('c.tsv', 'x,y\tz\n', false)).toEqual({ columns: ['1', '2'], rows: [['x,y', 'z']], delimiter: '\t' })
    expect(parseTable('empty.csv', '', true)).toEqual({ columns: [], rows: [], delimiter: ',' })
  })

  it('compares cells', () => {
    const compare = compareCells(new Intl.Collator('en', { numeric: true }))
    expect(['10', '9', '100'].sort(compare)).toEqual(['9', '10', '100'])
    expect(['b', 'A', 'a2', 'a10'].sort(compare)).toEqual(['A', 'a2', 'a10', 'b'])
    expect(compare('', '3')).toBeLessThan(0)
  })
})
