import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import initSqlJs from 'sql.js/dist/sql-asm.js'
import { beforeEach, describe, expect, it } from 'vitest'

import { maxQueryRows, pageSize, quoteName, renderSqlite } from './sqlite'

/** The words of the interface, as the parent hands them. */
const labels = {
  tables: 'Tables',
  view: 'view',
  rows: 'Rows {{from}}–{{to}} of {{total}}',
  previous: 'Previous',
  next: 'Next',
  structure: 'Structure',
  query: 'Query',
  queryPlaceholder: 'SELECT',
  run: 'Run',
  result: 'Result',
  resultRows: '{{count}} rows',
  resultCut: 'First {{count}} rows',
  blob: 'BLOB · {{size}} bytes',
  empty: 'No tables.',
}

/** Reads a SQLite sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/sqlite', name)))
}

/** Writes a database from statements. */
async function database(sql: string): Promise<Uint8Array> {
  const SQL = await initSqlJs()
  const db = new SQL.Database()
  db.exec(sql)
  return db.export()
}

/** The buttons of the page by their text. */
function button(root: HTMLElement, text: string): HTMLButtonElement {
  return Array.from(root.querySelectorAll('button')).find((node) => node.textContent?.startsWith(text)) as HTMLButtonElement
}

/** The text of the cells of the table shown, row by row. */
function cells(root: HTMLElement): string[][] {
  return Array.from(root.querySelectorAll('.sqlite-content tbody tr')).map((row) => Array.from(row.querySelectorAll('td')).map((cell) => cell.textContent as string))
}

let root: HTMLElement

beforeEach(() => {
  root = document.createElement('div')
})

/**
 * Covers {@link renderSqlite} (D2.7).
 *
 * Scenarios:
 * - the tables and views are listed, the first opens with its rows, NULLs, blobs, numbers and text, and its structure a click away
 * - a table of 20 000 rows is read by pages, forward and back
 * - a view opens like a table, with its mark in the list
 * - a query shows its result, cut at the most rows shown; an empty query does nothing; writes are refused, and errors are shown
 * - Ctrl+Enter runs the query and other keys do not
 * - a database without tables says so, an empty table counts no rows, a view whose table is gone shows its error where it fails, and a truncated database or a file that
 *   is not a database fails
 * - names are quoted for SQLite
 */
describe('renderSqlite', () => {
  it('browses the tables and views of a database', async () => {
    await renderSqlite(sample('sample.sqlite'), root, labels)
    expect(Array.from(root.querySelectorAll('.sqlite-object'), (node) => node.textContent)).toEqual(['events', 'people', 'settings', 'totalsview'])
    expect(root.querySelector('.sqlite-title')?.textContent).toBe('events')
    expect(root.querySelector('.sqlite-count')?.textContent).toBe(`Rows 1–${pageSize} of 20000`)
    expect(cells(root)).toHaveLength(pageSize)
    expect(button(root, 'Previous').disabled).toBe(true)
    button(root, 'Next').click()
    expect(root.querySelector('.sqlite-count')?.textContent).toBe(`Rows ${pageSize + 1}–${pageSize * 2} of 20000`)
    button(root, 'Previous').click()
    expect(root.querySelector('.sqlite-count')?.textContent).toBe(`Rows 1–${pageSize} of 20000`)

    button(root, 'people').click()
    expect(button(root, 'people').getAttribute('aria-current')).toBe('true')
    const people = cells(root)
    expect(people[0].slice(0, 5)).toEqual(['1', 'Ana', 'Santiago', '1200.5', '2026-09-02'])
    expect(people[0][5]).toMatch(/^BLOB · \d+ bytes$/)
    expect(people[3]).toEqual(['4', 'Diego', 'NULL', 'NULL', 'NULL', 'NULL'])
    expect(people[4][1]).toBe('Émilie 日本語 😀')
    expect(root.querySelector('.sqlite-number')?.textContent).toBe('1')
    expect(button(root, 'Next').disabled).toBe(true)
    const statement = root.querySelector('.sqlite-sql') as HTMLElement
    expect(statement.hidden).toBe(true)
    button(root, 'Structure').click()
    expect(statement.hidden).toBe(false)
    expect(statement.textContent).toContain('CREATE TABLE people')
    button(root, 'Structure').click()
    expect(statement.hidden).toBe(true)

    button(root, 'totals').click()
    expect(cells(root)).toContainEqual(['Santiago', '1', '1200.5'])
  })

  it('runs read-only queries', async () => {
    await renderSqlite(sample('sample.sqlite'), root, labels)
    const editor = root.querySelector('textarea') as HTMLTextAreaElement
    button(root, 'Run').click()
    expect(root.querySelector('.sqlite-title')?.textContent).toBe('events')
    editor.value = "SELECT name, city FROM people WHERE city LIKE 'S%'"
    button(root, 'Run').click()
    expect(root.querySelector('.sqlite-title')?.textContent).toBe('Result')
    expect(root.querySelector('.sqlite-count')?.textContent).toBe('1 rows')
    expect(cells(root)).toEqual([['Ana', 'Santiago']])
    expect(root.querySelector('[aria-current="true"]')).toBeNull()
    editor.value = 'SELECT * FROM events'
    editor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    expect(cells(root)).toHaveLength(1)
    editor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true }))
    expect(root.querySelector('.sqlite-count')?.textContent).toBe(`First ${maxQueryRows} rows`)
    expect(cells(root)).toHaveLength(maxQueryRows)
    editor.value = 'DELETE FROM people'
    editor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', metaKey: true }))
    expect(root.querySelector('[role="alert"]')?.textContent).toMatch(/readonly|read-only|query_only/i)
    editor.value = 'SELECT * FROM missing'
    button(root, 'Run').click()
    expect(root.querySelector('[role="alert"]')?.textContent).toContain('no such table')
  })

  it('handles empty, damaged and foreign files', async () => {
    await renderSqlite(await database(''), root, labels)
    expect(root.textContent).toContain('No tables.')
    const broken = document.createElement('div')
    await renderSqlite(await database('CREATE TABLE blank (x); CREATE TABLE gone (x); CREATE VIEW orphan AS SELECT x FROM gone; DROP TABLE gone;'), broken, labels)
    expect(broken.querySelector('.sqlite-count')?.textContent).toBe('Rows 0–0 of 0')
    button(broken, 'orphan').click()
    expect(broken.querySelector('[role="alert"]')?.textContent).toContain('no such table')
    await expect(renderSqlite(sample('truncated.sqlite'), document.createElement('div'), labels)).rejects.toThrow()
    await expect(renderSqlite(new TextEncoder().encode('SQLite format 3\u0000 but nothing else of a database'), document.createElement('div'), labels)).rejects.toThrow()
    expect(quoteName('a"b')).toBe('"a""b"')
  })
})
