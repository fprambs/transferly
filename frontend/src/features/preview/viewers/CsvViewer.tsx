import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { compareCells, maxTableRows, parseTable, tableLimit, type Table } from './table'
import { decodeText } from './text'
import type { ViewerProps } from './types'
import { WholeFile } from './WholeFile'

/** Height of a row, in pixels. */
const rowHeight = 28

/** The sort of the table: a column and its direction, or none. */
type Sort = { column: number; descending: boolean } | null

/** Names a delimiter for the bar. */
function delimiterName(delimiter: string): 'tab' | 'comma' | 'semicolon' | 'pipe' {
  return delimiter === '\t' ? 'tab' : delimiter === ',' ? 'comma' : delimiter === ';' ? 'semicolon' : 'pipe'
}

/** A table already parsed: its header, sortable columns and virtual rows. */
function TableView({ name, text }: { name: string; text: string }) {
  const { t, i18n } = useTranslation('preview')
  const [header, setHeader] = useState(true)
  const [sort, setSort] = useState<Sort>(null)
  const table: Table = useMemo(() => parseTable(name, text, header), [name, text, header])
  const rows = useMemo(() => {
    if (sort === null) {
      return table.rows
    }
    const compare = compareCells(new Intl.Collator(i18n.language, { numeric: true, sensitivity: 'base' }))
    const sorted = [...table.rows].sort((a, b) => compare(a[sort.column], b[sort.column]))
    return sort.descending ? sorted.reverse() : sorted
  }, [table, sort, i18n.language])
  const scroller = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => rowHeight, overscan: 12 })
  const template = `repeat(${Math.max(table.columns.length, 1)}, minmax(8rem, 1fr))`
  const sortBy = (column: number): void =>
    setSort(sort?.column === column ? (sort.descending ? null : { column, descending: true }) : { column, descending: false })

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-3 border-b px-3 py-1 text-xs text-muted-foreground">
        <span>{t(($) => $.table.summary, { rows: rows.length, columns: table.columns.length, delimiter: t(($) => $.table.delimiters[delimiterName(table.delimiter)]) })}</span>
        <Button variant={header ? 'secondary' : 'ghost'} size="xs" className="ms-auto" aria-pressed={header} onClick={() => (setHeader(!header), setSort(null))}>
          {t(($) => $.table.header)}
        </Button>
      </div>
      {table.cut && (
        <p role="status" className="border-b bg-warn/10 px-3 py-1 text-xs text-warn-text">
          {t(($) => $.table.cut, { shown: maxTableRows })}
        </p>
      )}
      <div ref={scroller} role="table" aria-label={t(($) => $.table.label, { name })} aria-rowcount={rows.length + 1} className="min-h-0 flex-1 overflow-auto bg-card text-xs">
        <div role="row" className="sticky top-0 z-10 grid border-b bg-muted font-medium" style={{ gridTemplateColumns: template }}>
          {table.columns.map((column, index) => (
            <button
              key={index}
              type="button"
              role="columnheader"
              aria-sort={sort?.column === index ? (sort.descending ? 'descending' : 'ascending') : 'none'}
              className="flex items-center gap-1 truncate border-e px-2 py-1.5 text-start hover:bg-muted/70"
              onClick={() => sortBy(index)}
            >
              <span className="truncate">{column}</span>
              {sort?.column === index && (sort.descending ? <ArrowDown aria-hidden="true" className="size-3" /> : <ArrowUp aria-hidden="true" className="size-3" />)}
            </button>
          ))}
        </div>
        <div role="rowgroup" className="relative" style={{ height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map((item) => (
            <div key={item.key} role="row" className="absolute inset-x-0 grid border-b" style={{ top: item.start, height: rowHeight, gridTemplateColumns: template }}>
              {rows[item.index].map((cell, column) => (
                <span key={column} role="cell" className="truncate border-e px-2 leading-7" title={cell}>
                  {cell}
                </span>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

/** Shows a CSV or TSV file as a table: the delimiter is guessed, the first row can be the header, and columns sort (ADR-0018). */
export function CsvViewer({ source }: ViewerProps) {
  return <WholeFile source={source} limit={tableLimit}>{(bytes) => <TableView name={source.name} text={decodeText(bytes)} />}</WholeFile>
}
