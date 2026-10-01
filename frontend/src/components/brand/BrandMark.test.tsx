import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { BrandMark } from './BrandMark'

/**
 * Covers {@link BrandMark}.
 *
 * Scenarios:
 * - the mark is hidden from assistive technology, since the product name is written next to it
 * - it takes the classes it is given
 * - the arrow and the ribbon read the brand colors, not the theme
 */
describe('BrandMark', () => {
  it('is decorative, sized by its classes and painted with the brand colors', () => {
    const { container } = render(<BrandMark className="size-6" />)
    const svg = container.querySelector('svg')

    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveClass('size-6')
    expect([...container.querySelectorAll('g')].map((group) => group.getAttribute('class'))).toEqual(['text-(--brand-arrow)', 'text-(--brand-ribbon)'])
  })
})
