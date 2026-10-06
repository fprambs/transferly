import { describe, expect, it } from 'vitest'

import { compareCells, maxTableRows, parseTable } from './table'

/**
 * Covers {@link parseTable} and {@link compareCells} (ADR-0018).
 *
 * Scenarios:
 * - the delimiter is guessed (comma, semicolon), TSV files use the tab, quoted cells keep their delimiter, and empty lines are left out
 * - the first row is the header, or columns are numbered without one; short rows are padded
 * - a long file is read up to its first rows, and says it was cut
 * - numbers sort as numbers and text in the natural order of the language
 */
describe('tables', () => {
  it('guesses the delimiter and pads rows', () => {
    expect(parseTable('a.csv', 'a,b,c\n1,"2,5"\n\n3,4,5\n', true)).toEqual({ columns: ['a', 'b', 'c'], rows: [['1', '2,5', ''], ['3', '4', '5']], delimiter: ',', cut: false })
    expect(parseTable('b.csv', 'x;y\n1;2\n', true).delimiter).toBe(';')
    expect(parseTable('c.tsv', 'x,y\tz\n', false)).toEqual({ columns: ['1', '2'], rows: [['x,y', 'z']], delimiter: '\t', cut: false })
    expect(parseTable('empty.csv', '', true)).toEqual({ columns: [], rows: [], delimiter: ',', cut: false })
  })

  it('reads the first rows of a long file only', () => {
    const lines = Array.from({ length: maxTableRows + 5 }, (_, index) => `${index},x`).join('\n')
    const withHeader = parseTable('long.csv', `n,v\n${lines}`, true)
    expect([withHeader.rows.length, withHeader.cut, withHeader.rows[0][0]]).toEqual([maxTableRows, true, '0'])
    const bare = parseTable('long.csv', lines, false)
    expect([bare.rows.length, bare.cut]).toEqual([maxTableRows, true])
    const exact = parseTable('exact.csv', Array.from({ length: maxTableRows }, (_, index) => `${index}`).join('\n'), false)
    expect([exact.rows.length, exact.cut]).toEqual([maxTableRows, false])
  })

  it('compares cells', () => {
    const compare = compareCells(new Intl.Collator('en', { numeric: true }))
    expect(['10', '9', '100'].sort(compare)).toEqual(['9', '10', '100'])
    expect(['b', 'A', 'a2', 'a10'].sort(compare)).toEqual(['A', 'a2', 'a10', 'b'])
    expect(compare('', '3')).toBeLessThan(0)
  })
})
