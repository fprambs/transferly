import { useQuery } from '@tanstack/react-query'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import type { PreviewSource } from '../source'

import type { ViewerProps } from './types'

/** Bytes shown on each row. */
const rowBytes = 16

/** Bytes read at once, as the rows of a page come into view. */
const pageBytes = 4096

/** Height of a row, in pixels. */
const rowHeight = 22

/** Writes a number as hexadecimal of `digits` digits. */
function hex(value: number, digits: number): string {
  return value.toString(16).padStart(digits, '0')
}

/** Writes a byte as the character it is when printable ASCII, or a dot. */
function printable(byte: number): string {
  return byte >= 0x20 && byte < 0x7f ? String.fromCharCode(byte) : '.'
}

/** Reads the page of a source that holds a row, cached while the viewer stays open. */
function usePage(source: PreviewSource, page: number) {
  return useQuery({
    queryKey: ['preview', source.key, 'hex', page],
    queryFn: ({ signal }) => source.read(page * pageBytes, (page + 1) * pageBytes, signal),
    staleTime: Infinity,
  })
}

/** One row of the view: its offset, its bytes in hexadecimal, and the same bytes as text. Its page is read when the row comes into view. */
function HexRow({ source, row, top }: { source: PreviewSource; row: number; top: number }) {
  const offset = row * rowBytes
  const page = Math.floor(offset / pageBytes)
  const { data } = usePage(source, page)
  const start = offset - page * pageBytes
  const bytes = data === undefined ? undefined : [...data.subarray(start, start + rowBytes)]
  const cells = Array.from({ length: rowBytes }, (_, index) => (bytes === undefined ? '··' : index < bytes.length ? hex(bytes[index], 2) : '  '))
  return (
    <div role="row" className="absolute inset-x-0 flex gap-6 px-4 font-mono text-xs leading-[22px] whitespace-pre" style={{ top, height: rowHeight }}>
      <span role="cell" className="text-muted-foreground tabular-nums">
        {hex(offset, 8)}
      </span>
      <span role="cell">{`${cells.slice(0, 8).join(' ')}  ${cells.slice(8).join(' ')}`}</span>
      <span role="cell" className="text-muted-foreground">
        {bytes === undefined ? '' : bytes.map(printable).join('')}
      </span>
    </div>
  )
}

/**
 * Shows any file as its bytes (ADR-0018): offset, hexadecimal and printable text, sixteen bytes a row. Rows are virtual and their bytes are
 * read in pages of 4 KiB only as they come into view, so a file of any size opens at once. It is the viewer of binary files and the
 * fallback of every other viewer.
 */
export function HexViewer({ source }: ViewerProps) {
  const { t } = useTranslation('preview')
  const scroller = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: Math.ceil(source.size / rowBytes),
    getScrollElement: () => scroller.current,
    estimateSize: () => rowHeight,
    overscan: 16,
  })
  if (source.size === 0) {
    return <p className="m-auto text-sm text-muted-foreground">{t(($) => $.empty)}</p>
  }
  return (
    <div ref={scroller} role="table" aria-label={t(($) => $.hex.label, { name: source.name })} className="min-h-0 flex-1 overflow-auto bg-card">
      <div role="rowgroup" className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((item) => (
          <HexRow key={item.key} source={source} row={item.index} top={item.start} />
        ))}
      </div>
    </div>
  )
}
