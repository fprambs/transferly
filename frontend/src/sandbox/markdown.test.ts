import { describe, expect, it } from 'vitest'

import { renderMarkdown } from './markdown'

/**
 * Covers {@link renderMarkdown} (ADR-0018).
 *
 * Scenarios:
 * - Markdown becomes HTML with headings, tables, code and links
 * - scripts, handlers, frames, forms, styles and javascript links are removed by the sanitizer
 * - clicking a link does not navigate
 */
describe('renderMarkdown', () => {
  it('renders and sanitizes', async () => {
    const root = document.createElement('main')
    await renderMarkdown(
      '# Title\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n`code` [site](https://example.com) [bad](javascript:alert(1))\n\n<script>alert(1)</script><img src=x onerror="alert(1)"><iframe src="https://example.com"></iframe><form><input></form><style>body{}</style>',
      root,
    )
    expect(root.classList.contains('markdown')).toBe(true)
    expect(root.querySelector('h1')?.textContent).toBe('Title')
    expect(root.querySelector('table td')?.textContent).toBe('1')
    expect(root.querySelector('script, iframe, form, input, style, [onerror]')).toBeNull()
    expect(root.querySelector('a[href^="javascript"]')).toBeNull()

    const link = root.querySelector('a[href="https://example.com"]') as HTMLAnchorElement
    const click = new MouseEvent('click', { bubbles: true, cancelable: true })
    link.dispatchEvent(click)
    expect(click.defaultPrevented).toBe(true)
    const heading = new MouseEvent('click', { bubbles: true, cancelable: true })
    root.querySelector('h1')?.dispatchEvent(heading)
    expect(heading.defaultPrevented).toBe(false)
  })
})
