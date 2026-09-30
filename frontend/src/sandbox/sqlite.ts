import type { Database, SqlValue } from 'sql.js'
import initSqlJs from 'sql.js/dist/sql-asm.js'

import { element, fill } from './office/common'

/** How many rows of a table a page shows. */
export const pageSize = 200

/** The most rows a query shows. */
export const maxQueryRows = 1000

/** A table or a view of the database. */
interface DatabaseObject {
  /** Whether it is a table or a view. */
  type: string
  /** Its name. */
  name: string
  /** The statement that created it. */
  sql: string
}

/** Quotes an identifier for SQLite, doubling its quotes. */
export function quoteName(name: string): string {
  return `"${name.replaceAll('"', '""')}"`
}

/** Draws one value: NULL and blobs as labels, numbers aligned to the right, and text as text. */
function drawValue(value: SqlValue, labels: Record<string, string>): HTMLTableCellElement {
  if (value === null) {
    return element('td', 'sqlite-null', 'NULL')
  }
  if (value instanceof Uint8Array) {
    return element('td', 'sqlite-blob', fill(labels.blob, { size: value.length }))
  }
  const cell = element('td', typeof value === 'string' ? undefined : 'sqlite-number', String(value))
  cell.title = String(value)
  return cell
}

/** Draws rows as a table with their column names. */
function drawRows(columns: string[], rows: SqlValue[][], labels: Record<string, string>): HTMLTableElement {
  const table = element('table', 'sheet')
  const head = element('tr')
  head.append(...columns.map((column) => element('th', undefined, column)))
  table.appendChild(element('thead')).append(head)
  const body = table.appendChild(element('tbody'))
  for (const row of rows) {
    const line = element('tr')
    line.append(...row.map((value) => drawValue(value, labels)))
    body.append(line)
  }
  return table
}

/** Runs a statement and collects its column names and at most `limit` rows. */
function query(db: Database, sql: string, limit: number): { columns: string[]; rows: SqlValue[][]; more: boolean } {
  const statement = db.prepare(sql)
  try {
    const rows: SqlValue[][] = []
    let more = false
    while (statement.step()) {
      if (rows.length === limit) {
        more = true
        break
      }
      rows.push(statement.get())
    }
    return { columns: statement.getColumnNames(), rows, more }
  } finally {
    statement.free()
  }
}

/**
 * Browses a SQLite database inside the isolated page (ADR-0018) with sql.js, in its asm.js build, so the page needs no WebAssembly: the
 * tables and views in a side list, the rows of the one chosen by pages of {@link pageSize}, the statement that created it, and a box to run
 * queries, of which at most {@link maxQueryRows} rows are shown. The database is a copy in memory opened with `PRAGMA query_only`, so
 * nothing is written, neither to the file nor to the copy. A file that is not a database fails; a damaged one says so where it fails.
 */
export async function renderSqlite(bytes: Uint8Array, root: HTMLElement, labels: Record<string, string>): Promise<void> {
  const SQL = await initSqlJs()
  const db = new SQL.Database(bytes)
  db.exec('PRAGMA query_only = ON')
  const listed = db.exec("SELECT type, name, sql FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\' ORDER BY type, name")
  const objects: DatabaseObject[] = (listed[0]?.values ?? []).map(([type, name, sql]) => ({ type: String(type), name: String(name), sql: String(sql) }))

  root.classList.add('sqlite')
  const side = element('nav', 'sqlite-side')
  side.setAttribute('aria-label', labels.tables)
  const main = element('section', 'sqlite-main')
  const content = element('div', 'sqlite-content')
  const editor = element('textarea', 'sqlite-query')
  editor.setAttribute('aria-label', labels.query)
  editor.placeholder = labels.queryPlaceholder
  editor.spellcheck = false
  const run = element('button', 'pdf-button', labels.run)
  run.type = 'button'
  const queryBar = element('div', 'sqlite-query-bar')
  queryBar.append(editor, run)
  main.append(queryBar, content)
  root.append(side, main)

  const showError = (error: unknown): void => {
    const message = element('p', 'sqlite-error', (error as Error).message)
    message.setAttribute('role', 'alert')
    content.replaceChildren(message)
  }

  const buttons = objects.map((object) => {
    const button = element('button', 'sqlite-object')
    button.type = 'button'
    button.append(element('span', undefined, object.name))
    if (object.type === 'view') {
      button.append(element('span', 'sqlite-kind', labels.view))
    }
    button.addEventListener('click', () => open(object, 0))
    return button
  })

  const open = (object: DatabaseObject, page: number): void => {
    buttons.forEach((button, index) => button.setAttribute('aria-current', String(objects[index] === object)))
    let total: number
    let rows
    try {
      total = Number(query(db, `SELECT COUNT(*) FROM ${quoteName(object.name)}`, 1).rows[0][0])
      rows = query(db, `SELECT * FROM ${quoteName(object.name)} LIMIT ${pageSize} OFFSET ${page * pageSize}`, pageSize)
    } catch (error) {
      showError(error)
      return
    }
    const header = element('div', 'sqlite-header')
    header.append(element('h2', 'sqlite-title', object.name))
    const from = total === 0 ? 0 : page * pageSize + 1
    header.append(element('span', 'sqlite-count', fill(labels.rows, { from, to: page * pageSize + rows.rows.length, total })))
    const previous = element('button', 'pdf-button', labels.previous)
    previous.type = 'button'
    previous.disabled = page === 0
    previous.addEventListener('click', () => open(object, page - 1))
    const next = element('button', 'pdf-button', labels.next)
    next.type = 'button'
    next.disabled = (page + 1) * pageSize >= total
    next.addEventListener('click', () => open(object, page + 1))
    const structure = element('button', 'pdf-button', labels.structure)
    structure.type = 'button'
    structure.setAttribute('aria-pressed', 'false')
    const statement = element('pre', 'sqlite-sql', object.sql)
    statement.hidden = true
    structure.addEventListener('click', () => {
      statement.hidden = !statement.hidden
      structure.setAttribute('aria-pressed', String(!statement.hidden))
    })
    header.append(previous, next, structure)
    content.replaceChildren(header, statement, drawRows(rows.columns, rows.rows, labels))
  }

  const runQuery = (): void => {
    const sql = editor.value.trim()
    if (sql === '') {
      return
    }
    buttons.forEach((button) => button.setAttribute('aria-current', 'false'))
    try {
      const result = query(db, sql, maxQueryRows)
      const header = element('div', 'sqlite-header')
      header.append(element('h2', 'sqlite-title', labels.result))
      header.append(element('span', 'sqlite-count', fill(result.more ? labels.resultCut : labels.resultRows, { count: result.rows.length })))
      content.replaceChildren(header, drawRows(result.columns, result.rows, labels))
    } catch (error) {
      showError(error)
    }
  }
  run.addEventListener('click', runQuery)
  editor.addEventListener('keydown', (event) => {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault()
      runQuery()
    }
  })

  side.append(element('p', 'sqlite-side-title', labels.tables), ...buttons)
  if (objects.length === 0) {
    content.append(element('p', 'sheet-empty', labels.empty))
    return
  }
  open(objects[0], 0)
}
