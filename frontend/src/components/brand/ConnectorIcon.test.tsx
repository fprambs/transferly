import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ConnectorIcon } from './ConnectorIcon'

/**
 * Covers {@link ConnectorIcon}.
 *
 * Scenarios:
 * - each icon a built-in connector names is drawn with its own artwork, and is decorative
 * - two icons on one page give their gradients ids of their own
 * - an icon the interface does not know gets the generic connector icon, in the color of the text
 */
describe('ConnectorIcon', () => {
  it.each([
    ['sftp', '0 0 48 48'],
    ['ftps', '0 0 48 48'],
    ['webdav', '0 0 48 48'],
    ['cloud', '0 0 64 64'],
    ['gcs', '0 0 512 512'],
    ['gdrive', '0 -4.65 87.3 87.3'],
    ['azblob', '0 0 18 18'],
    ['azfiles', '0 0 18 18'],
  ])('draws the %s icon', (icon, viewBox) => {
    const { container } = render(<ConnectorIcon icon={icon} className="size-6" />)
    const svg = container.querySelector('svg')
    expect(svg).toHaveAttribute('data-icon', icon)
    expect(svg).toHaveAttribute('viewBox', viewBox)
    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveClass('size-6')
  })

  it('keeps the gradients of two icons apart', () => {
    const { container } = render(
      <>
        <ConnectorIcon icon="azblob" />
        <ConnectorIcon icon="azblob" />
      </>,
    )
    const ids = [...container.querySelectorAll('linearGradient')].map((gradient) => gradient.id)
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
    for (const id of ids) {
      expect(id).toMatch(/^[a-zA-Z0-9_-]+$/)
      expect(container.querySelector(`[fill="url(#${id})"]`)).not.toBeNull()
    }
  })

  it('draws a generic icon for an unknown one', () => {
    const { container } = render(<ConnectorIcon icon="bucket" />)
    expect(container.querySelector('svg')).toHaveAttribute('data-icon', 'connector')
    expect(container.querySelector('svg g')).toHaveAttribute('stroke', 'currentColor')
  })
})
