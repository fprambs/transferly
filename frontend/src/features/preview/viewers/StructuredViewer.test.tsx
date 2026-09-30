import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { textSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { StructuredViewer } from './StructuredViewer'

/**
 * Covers {@link StructuredViewer} and the {@link TreeView} it draws (ADR-0018).
 *
 * Scenarios:
 * - JSON shows as a tree whose first levels are open, with typed values; nodes close and open again
 * - long lists show 200 children and offer the rest
 * - XML shows elements with their attributes and text
 * - the source is a click away; a file that does not parse says why and shows only its source
 */
describe('StructuredViewer', () => {
  it('shows JSON as a tree', async () => {
    const list = JSON.stringify({ items: Array.from({ length: 250 }, (_, index) => index), deep: { a: { b: { c: 'hidden' } } } })
    await renderWithProviders(<StructuredViewer source={textSource('data.json', list)} kind="json" />)
    const tree = await screen.findByRole('tree', { name: 'Structure of data.json' })
    expect(tree).toHaveTextContent('items:[250]')
    expect(tree).not.toHaveTextContent('hidden')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Show 50 more' }))
    expect(tree).toHaveTextContent('249')
    const deep = within(tree).getAllByRole('treeitem').find((item) => item.textContent?.startsWith('c:') === false && item.getAttribute('aria-expanded') === 'false') as HTMLElement
    await user.click(within(deep).getAllByRole('button', { name: 'Expand' })[0])
    expect(tree).toHaveTextContent('"hidden"')
    await user.click(within(tree).getAllByRole('button', { name: 'Collapse' })[0])
    expect(tree).not.toHaveTextContent('items')

    await user.click(screen.getByRole('button', { name: 'Source' }))
    await waitFor(() => expect(screen.getByLabelText('Contents of data.json')).toHaveTextContent('"items"'))
  })

  it('shows XML elements', async () => {
    await renderWithProviders(<StructuredViewer source={textSource('feed.xml', '<rss version="2"><title>News</title></rss>')} kind="xml" />)
    const tree = await screen.findByRole('tree', { name: 'Structure of feed.xml' })
    expect(tree).toHaveTextContent('<rss version="2">')
    expect(tree).toHaveTextContent('News')
  })

  it('says what does not parse', async () => {
    await renderWithProviders(<StructuredViewer source={textSource('config.yaml', 'a: [unclosed')} kind="yaml" />)
    expect(await screen.findByRole('alert')).toHaveTextContent('This file does not parse')
    expect(screen.getByRole('button', { name: 'Tree' })).toBeDisabled()
    await waitFor(() => expect(screen.getByLabelText('Contents of config.yaml')).toHaveTextContent('unclosed'))
  })
})
