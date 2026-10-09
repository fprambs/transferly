import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { beforeEach, describe, expect, it } from 'vitest'
import { CFB, utils, write } from 'xlsx'

import { ProtectedDocument } from './common'
import { maxColumns, maxRows, renderSpreadsheet } from './spreadsheet'

/** The words of the interface, as the parent hands them. */
const labels = {
  sheets: 'Sheets',
  rowsCut: 'First {{shown}} of {{total}} rows.',
  columnsCut: 'First {{shown}} of {{total}} columns.',
  emptySheet: 'Empty sheet.',
}

/** Reads an Office sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/office', name)))
}

/** Writes a workbook with one sheet per array of rows. */
function book(sheets: Record<string, unknown[][]>): Uint8Array {
  const workbook = utils.book_new()
  for (const [name, rows] of Object.entries(sheets)) {
    utils.book_append_sheet(workbook, utils.aoa_to_sheet(rows), name)
  }
  return new Uint8Array(write(workbook, { type: 'array', bookType: 'xlsx' }) as ArrayBuffer)
}

/** The text of the cells of the table shown, row by row, without the numbers of the rows. */
function cells(root: HTMLElement): string[][] {
  return Array.from(root.querySelectorAll('tbody tr')).map((row) => Array.from(row.querySelectorAll('td')).map((cell) => cell.textContent as string))
}

let root: HTMLElement

beforeEach(() => {
  root = document.createElement('div')
})

/**
 * Covers {@link renderSpreadsheet} (D2.6).
 *
 * Scenarios:
 * - an Excel workbook shows one tab per sheet and the first sheet as a table with the letters of its columns and the numbers of its rows,
 *   its merged cells spanning, numbers and dates as the workbook formats them, and formulas as the results the file saved
 * - the tabs switch sheets, and a sheet of 5000 rows shows its first rows only, saying how many it has
 * - legacy Excel and OpenDocument workbooks show too
 * - sheets over the caps of rows and columns show the first ones and say so; an empty sheet says so
 * - a workbook protected with a password fails as protected, and a damaged one fails as it is
 */
describe('renderSpreadsheet', () => {
  it('shows an Excel workbook sheet by sheet', async () => {
    await renderSpreadsheet(sample('workbook.xlsx'), root, labels)
    const tabs = Array.from(root.querySelectorAll<HTMLButtonElement>('[role="tab"]'))
    expect(tabs.map((tab) => tab.textContent)).toEqual(['Sales', 'Notes', 'Big'])
    expect(tabs[0].getAttribute('aria-selected')).toBe('true')
    expect(root.querySelector('[role="tablist"]')?.getAttribute('aria-label')).toBe('Sheets')
    expect(Array.from(root.querySelectorAll('thead th')).map((cell) => cell.textContent)).toEqual(['', 'A', 'B', 'C', 'D'])
    const merged = root.querySelector('tbody td') as HTMLTableCellElement
    expect([merged.textContent, merged.colSpan]).toEqual(['Sales by person', 4])
    expect(cells(root)[2]).toEqual(['Ana', 'Santiago', '1,200.50', '2026-09-02'])
    expect(cells(root)[6]).toEqual(['', 'Total', '4,471.50', ''])
    tabs[2].click()
    expect(tabs[2].getAttribute('aria-selected')).toBe('true')
    expect(cells(root)).toHaveLength(maxRows)
    expect(root.textContent).toContain(`First ${maxRows} of 5001 rows.`)
    expect(cells(root)[3]).toEqual(['3', '4.5', '9'])
    tabs[1].click()
    expect(root.textContent).toContain('a formula link')
  })

  it('shows legacy Excel and OpenDocument workbooks', async () => {
    await renderSpreadsheet(sample('legacy.xls'), root, labels)
    expect(cells(root)[1]).toEqual(['Ana', 'Santiago', '1200.5'])
    const ods = document.createElement('div')
    await renderSpreadsheet(sample('sheet.ods'), ods, labels)
    expect(Array.from(ods.querySelectorAll('[role="tab"]')).map((tab) => tab.textContent)).toEqual(['People', 'Cities'])
  })

  it('cuts large sheets and says when a sheet is empty', async () => {
    const tall = Array.from({ length: maxRows + 5 }, (_, index) => [index])
    const wide = [Array.from({ length: maxColumns + 5 }, (_, index) => index)]
    await renderSpreadsheet(book({ Tall: tall, Wide: wide, Empty: [] }), root, labels)
    expect(root.textContent).toContain(`First ${maxRows} of ${maxRows + 5} rows.`)
    expect(cells(root)).toHaveLength(maxRows)
    const tabs = root.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    tabs[1].click()
    expect(root.textContent).toContain(`First ${maxColumns} of ${maxColumns + 5} columns.`)
    expect(cells(root)[0]).toHaveLength(maxColumns)
    tabs[2].click()
    expect(root.textContent).toContain('Empty sheet.')
  })

  it('refuses protected and damaged workbooks', async () => {
    const container = CFB.utils.cfb_new()
    CFB.utils.cfb_add(container, '/EncryptionInfo', new Uint8Array([4, 0, 4, 0, 0x40, 0, 0, 0]))
    CFB.utils.cfb_add(container, '/EncryptedPackage', new Uint8Array(64))
    const locked = new Uint8Array(CFB.write(container, { type: 'array' }) as ArrayBuffer)
    await expect(renderSpreadsheet(locked, root, labels)).rejects.toThrow(ProtectedDocument)
    const whole = sample('workbook.xlsx')
    await expect(renderSpreadsheet(whole.slice(0, whole.length / 2), root, labels)).rejects.toThrow()
    await expect(renderSpreadsheet(whole.slice(0, whole.length / 2), root, labels)).rejects.not.toThrow(ProtectedDocument)
    await expect(renderSpreadsheet(new Uint8Array([0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1]), root, labels)).rejects.not.toThrow(ProtectedDocument)
  })
})
