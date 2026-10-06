import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { textSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { CsvViewer } from './CsvViewer'
import { maxTableRows } from './table'

/** The cells of the first column, top to bottom. */
function firstColumn(table: HTMLElement): string[] {
  return within(table)
    .getAllByRole('row')
    .slice(1)
    .map((row) => within(row).getAllByRole('cell')[0].textContent as string)
}

/**
 * Covers {@link CsvViewer} (ADR-0018).
 *
 * Scenarios:
 * - the bar tells rows, columns and delimiter, and the first row is the header
 * - a column sorts up, then down, then back to the order of the file
 * - without the header the first row is data and the columns are numbered
 * - tabs, semicolons and pipes are named
 */
describe('CsvViewer', () => {
  it('shows a table that sorts', async () => {
    await renderWithProviders(<CsvViewer source={textSource('t.csv', 'n,name\n10,b\n9,c\n100,a\n')} kind="csv" />)
    const table = await screen.findByRole('table', { name: 'Table of t.csv' })
    expect(screen.getByText('3 rows · 2 columns · separated by commas')).toBeInTheDocument()
    expect(firstColumn(table)).toEqual(['10', '9', '100'])
    const user = userEvent.setup()
    const n = screen.getByRole('columnheader', { name: 'n' })
    await user.click(n)
    expect(firstColumn(table)).toEqual(['9', '10', '100'])
    await user.click(n)
    expect(n).toHaveAttribute('aria-sort', 'descending')
    expect(firstColumn(table)).toEqual(['100', '10', '9'])
    await user.click(n)
    expect(firstColumn(table)).toEqual(['10', '9', '100'])

    await user.click(screen.getByRole('button', { name: 'First row is the header' }))
    expect(screen.getByRole('columnheader', { name: '1' })).toBeInTheDocument()
    expect(firstColumn(table)[0]).toBe('n')
  })

  it('shows the first rows of a long file and says so', async () => {
    const lines = Array.from({ length: maxTableRows + 3 }, (_, index) => `${index},x`).join('\n')
    await renderWithProviders(<CsvViewer source={textSource('long.csv', `n,v\n${lines}`)} kind="csv" />)
    expect(await screen.findByText(`Showing the first ${maxTableRows} rows. Download the file to see them all.`)).toBeInTheDocument()
    expect(screen.getByText(`${maxTableRows} rows · 2 columns · separated by commas`)).toBeInTheDocument()
  })

  it('names the delimiters', async () => {
    await renderWithProviders(
      <>
        <CsvViewer source={textSource('a.tsv', 'a\tb\n1\t2\n')} kind="csv" />
        <CsvViewer source={textSource('b.csv', 'a;b\n1;2\n')} kind="csv" />
        <CsvViewer source={textSource('c.csv', 'a|b\n1|2\n')} kind="csv" />
      </>,
    )
    expect(await screen.findByText(/separated by tabs/)).toBeInTheDocument()
    expect(screen.getByText(/separated by semicolons/)).toBeInTheDocument()
    expect(screen.getByText(/separated by pipes/)).toBeInTheDocument()
  })
})
