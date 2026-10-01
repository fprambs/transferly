import { render, screen } from '@testing-library/react'
import { Users } from 'lucide-react'
import { describe, expect, it } from 'vitest'

import { DataTable, Td, Th, Tr } from './DataTable'
import { Badge, EmptyCard, Page, PageHeader, Section, StatTile, type Tone } from './Page'

/**
 * Covers the building blocks of full pages: {@link PageHeader}, {@link Section}, {@link StatTile}, {@link Badge} and the table cells.
 *
 * Scenarios:
 * - the page frames its content; the header names the page as its only first-level heading, with its description, its actions and what it
 *   holds below
 * - an empty card heads its state, with its explanation and actions only when given
 * - a section names itself by its heading, keeps the padding of the card unless its body is flush, and shows its description, actions and
 *   footer only when given
 * - a tile shows its count and label in every tone, and a badge its text with or without a leading dot
 * - a table keeps its header cells as column headings
 */
describe('page blocks', () => {
  it('heads a page', () => {
    render(
      <Page>
        <PageHeader icon={Users} title="People" description="Who uses this." actions={<button type="button">Invite</button>}>
          <p>Tabs</p>
        </PageHeader>
      </Page>,
    )

    expect(screen.getByRole('heading', { level: 1, name: 'People' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Invite' })).toBeInTheDocument()
    expect(screen.getByText('Who uses this.')).toBeInTheDocument()
    expect(screen.getByText('Tabs')).toBeInTheDocument()
  })

  it('tells an empty state', () => {
    const { rerender } = render(<EmptyCard icon={Users} title="Nobody yet" />)
    expect(screen.getByRole('heading', { name: 'Nobody yet' })).toBeInTheDocument()

    rerender(
      <EmptyCard icon={Users} title="Nobody yet" body="Invite someone.">
        <button type="button">Invite</button>
      </EmptyCard>,
    )
    expect(screen.getByText('Invite someone.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Invite' })).toBeInTheDocument()
  })

  it('draws sections with and without their extras', () => {
    const { rerender } = render(
      <Section id="s" title="Plain">
        <p>Body</p>
      </Section>,
    )
    expect(screen.getByRole('region', { name: 'Plain' })).toBeInTheDocument()
    expect(screen.getByText('Body').parentElement).toHaveClass('px-5')

    rerender(
      <Section id="s" title="Full" description="What it holds." actions={<button type="button">Act</button>} footer={<span>Done</span>} flush>
        <p>Body</p>
      </Section>,
    )
    expect(screen.getByText('What it holds.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Act' })).toBeInTheDocument()
    expect(screen.getByText('Done')).toBeInTheDocument()
    expect(screen.getByText('Body').parentElement).not.toHaveClass('px-5')
  })

  it.each<Tone>(['accent', 'ok', 'warn', 'danger', 'info', 'muted'])('tints a tile and a badge in the %s tone', (tone) => {
    const { container } = render(
      <>
        <StatTile icon={Users} label="Accounts" value={3} tone={tone} />
        <Badge tone={tone} dot>
          State
        </Badge>
      </>,
    )

    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('Accounts')).toBeInTheDocument()
    expect(screen.getByText('State').querySelector('.bg-current')).toBeInTheDocument()
    expect(container.innerHTML).toContain(tone === 'danger' ? 'bg-destructive/12' : tone === 'muted' ? 'bg-muted' : tone === 'accent' ? 'bg-primary/12' : `bg-${tone}/12`)
  })

  it('draws a tile in the brand tone and a badge without a dot by default', () => {
    render(
      <>
        <StatTile icon={Users} label="Accounts" value={1} />
        <Badge tone="ok">Plain</Badge>
      </>,
    )

    expect(screen.getByText('1').parentElement?.previousElementSibling).toHaveClass('bg-primary/12')
    expect(screen.getByText('Plain').querySelector('.bg-current')).toBeNull()
  })

  it('lays out a table with column headings', () => {
    render(
      <DataTable aria-label="Things">
        <thead>
          <tr>
            <Th>Name</Th>
          </tr>
        </thead>
        <tbody>
          <Tr>
            <Td>One</Td>
          </Tr>
        </tbody>
      </DataTable>,
    )

    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'One' })).toBeInTheDocument()
  })
})
