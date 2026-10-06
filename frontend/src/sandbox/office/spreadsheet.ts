import { read, utils, type Range, type WorkSheet } from 'xlsx'

import { element, fill, ProtectedDocument } from './common'

/** The most rows of a sheet the viewer reads and draws. */
export const maxRows = 1_000

/** The most columns of a sheet the viewer draws. */
export const maxColumns = 200

/** Tells whether a failure of SheetJS means the workbook is protected with a password. */
function isProtected(error: unknown): boolean {
  return error instanceof Error && /password|encrypt/i.test(error.message)
}

/** The cells a merge covers, besides its first one, and the spans of the first ones, by `row:column`. */
function mergesOf(sheet: WorkSheet): { spans: Map<string, Range>; covered: Set<string> } {
  const spans = new Map<string, Range>()
  const covered = new Set<string>()
  for (const merge of sheet['!merges'] ?? []) {
    spans.set(`${merge.s.r}:${merge.s.c}`, merge)
    for (let row = merge.s.r; row <= merge.e.r; row++) {
      for (let column = merge.s.c; column <= merge.e.c; column++) {
        if (row !== merge.s.r || column !== merge.s.c) {
          covered.add(`${row}:${column}`)
        }
      }
    }
  }
  return { spans, covered }
}

/** Draws one sheet as a table with the letters of its columns and the numbers of its rows, as a spreadsheet does. */
function drawSheet(sheet: WorkSheet, labels: Record<string, string>): HTMLElement {
  const box = element('div', 'sheet-box')
  const rows = utils.sheet_to_json<string[]>(sheet, { header: 1, raw: false, defval: '', blankrows: true })
  const full = utils.decode_range(sheet['!fullref'] ?? sheet['!ref'] ?? 'A1')
  const width = Math.min(full.e.c + 1, maxColumns)
  const notices: string[] = []
  if (full.e.r + 1 > rows.length && rows.length >= maxRows) {
    notices.push(fill(labels.rowsCut, { shown: maxRows, total: full.e.r + 1 }))
  }
  if (full.e.c + 1 > maxColumns) {
    notices.push(fill(labels.columnsCut, { shown: maxColumns, total: full.e.c + 1 }))
  }
  for (const notice of notices) {
    box.append(element('p', 'sheet-notice', notice))
  }
  if (rows.length === 0) {
    box.append(element('p', 'sheet-empty', labels.emptySheet))
    return box
  }
  const { spans, covered } = mergesOf(sheet)
  const table = element('table', 'sheet')
  const head = element('tr')
  head.append(element('th', 'sheet-corner'))
  for (let column = 0; column < width; column++) {
    head.append(element('th', undefined, utils.encode_col(column)))
  }
  table.appendChild(element('thead')).append(head)
  const body = table.appendChild(element('tbody'))
  rows.forEach((values, row) => {
    const line = element('tr')
    line.append(element('th', undefined, String(row + 1)))
    for (let column = 0; column < width; column++) {
      if (covered.has(`${row}:${column}`)) {
        continue
      }
      const cell = element('td', undefined, values[column] ?? '')
      const span = spans.get(`${row}:${column}`)
      if (span !== undefined) {
        cell.rowSpan = span.e.r - span.s.r + 1
        cell.colSpan = Math.min(span.e.c, width - 1) - span.s.c + 1
      }
      line.append(cell)
    }
    body.append(line)
  })
  box.append(table)
  return box
}

/**
 * Renders a workbook (xlsx, xls, ods) inside the isolated page with SheetJS (ADR-0018): one tab per sheet, and each sheet as a table of
 * the text its cells show, formatted as the workbook formats it. Nothing is computed or run: formulas show the results the file saved,
 * macros, styles and the HTML of cells are not read, and at most {@link maxRows} rows and {@link maxColumns} columns are drawn, saying
 * so when a sheet has more. A workbook protected with a password fails with {@link ProtectedDocument}.
 */
export function renderSpreadsheet(bytes: Uint8Array, root: HTMLElement, labels: Record<string, string>): void {
  let book
  try {
    book = read(bytes, { type: 'array', dense: true, sheetRows: maxRows, cellFormula: false, cellHTML: false, cellStyles: false, bookVBA: false })
  } catch (error) {
    throw isProtected(error) ? new ProtectedDocument() : error
  }
  root.classList.add('sheets')
  const tabs = element('div', 'sheet-tabs')
  tabs.setAttribute('role', 'tablist')
  tabs.setAttribute('aria-label', labels.sheets)
  const view = element('div', 'sheet-view')
  const buttons = book.SheetNames.map((name, index) => {
    const button = element('button', 'sheet-tab', name)
    button.type = 'button'
    button.setAttribute('role', 'tab')
    button.addEventListener('click', () => show(index))
    return button
  })
  const show = (index: number): void => {
    buttons.forEach((button, at) => button.setAttribute('aria-selected', String(at === index)))
    view.replaceChildren(drawSheet(book.Sheets[book.SheetNames[index]], labels))
  }
  tabs.append(...buttons)
  root.append(tabs, view)
  show(0)
}
