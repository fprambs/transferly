import Papa from 'papaparse'

/** The largest table file read, in bytes; the rows are drawn virtually, so the limit is the parse. */
export const tableLimit = 20 * 1024 * 1024

/** The most rows of a table read and drawn; a larger file says so and can be downloaded whole. */
export const maxTableRows = 1_000

/** A table read from a CSV or TSV file. */
export interface Table {
  /** The names of the columns: the first row, or numbers when the file has no header. */
  columns: string[]
  /** The rows under the header, each as long as the widest row. */
  rows: string[][]
  /** The delimiter found, such as a comma or a tab. */
  delimiter: string
  /** Whether the file has more rows than {@link maxTableRows}, which were left out. */
  cut: boolean
}

/**
 * Reads a CSV or TSV file into a table. The delimiter is guessed among comma, tab, pipe and semicolon, except for `.tsv` files, which use
 * the tab; empty lines are left out and short rows are padded so every row has every column. Only the first {@link maxTableRows} rows
 * under the header are read, so a long file is quick to show and does not freeze the page.
 */
export function parseTable(name: string, text: string, header: boolean): Table {
  const limit = maxTableRows + (header ? 1 : 0)
  const parsed = Papa.parse<string[]>(text, { skipEmptyLines: true, delimiter: /\.tsv$/i.test(name) ? '\t' : '', preview: limit + 1 })
  const cut = parsed.data.length > limit
  const data = cut ? parsed.data.slice(0, limit) : parsed.data
  const width = Math.max(0, ...data.map((row) => row.length))
  const pad = (row: string[]): string[] => [...row, ...Array<string>(width - row.length).fill('')]
  const first = data[0] as string[] | undefined
  const columns = header && first !== undefined ? pad(first) : Array.from({ length: width }, (_, index) => String(index + 1))
  return { columns, rows: (header ? data.slice(1) : data).map(pad), delimiter: parsed.meta.delimiter, cut }
}

/** Compares two cells as numbers when both are, and as text in the natural order of the interface language otherwise. */
export function compareCells(collator: Intl.Collator): (a: string, b: string) => number {
  return (a, b) => {
    const left = Number(a)
    const right = Number(b)
    return a.trim() !== '' && b.trim() !== '' && !Number.isNaN(left) && !Number.isNaN(right) ? left - right : collator.compare(a, b)
  }
}
