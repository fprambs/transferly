import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { BrandMark } from './BrandMark'

/**
 * Covers {@link BrandMark}.
 *
 * Scenarios:
 * - the mark is hidden from assistive technology, since the product name is written next to it
 * - it takes the classes it is given
 * - the arrow and the ribbon read the brand colors, not the theme, and the arrow turns light on the brand panel
 * - when animated, the strokes draw themselves and the arrowheads fade in, over and over or once; otherwise nothing moves
 */
describe('BrandMark', () => {
  it('is decorative, sized by its classes and painted with the brand colors', () => {
    const { container } = render(<BrandMark className="size-6" />)
    const svg = container.querySelector('svg')

    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveClass('size-6')
    expect(svg).not.toHaveAttribute('data-animated')
    expect(container.querySelectorAll('.brand-draw, .brand-fade')).toHaveLength(0)
    expect([...container.querySelectorAll('g')].map((group) => group.getAttribute('class'))).toEqual(['text-(--brand-arrow)', 'text-(--brand-ribbon)'])
  })

  it('turns its arrow light on the brand panel and draws itself when animated', () => {
    const { container } = render(<BrandMark surface="brand" animated />)

    expect(container.querySelector('svg')).toHaveAttribute('data-animated', 'true')
    expect(container.querySelector('g')).toHaveClass('text-(--brand-on-panel)')
    expect(container.querySelectorAll('.brand-draw')).toHaveLength(2)
    expect(container.querySelectorAll('.brand-fade')).toHaveLength(2)
  })

  it('draws itself once to greet', () => {
    const { container } = render(<BrandMark animated="once" />)

    expect(container.querySelector('svg')).toHaveAttribute('data-animated', 'once')
    expect(container.querySelectorAll('.brand-draw-once')).toHaveLength(2)
    expect(container.querySelectorAll('.brand-fade-once')).toHaveLength(2)
    expect(container.querySelectorAll('.brand-draw')).toHaveLength(0)
  })
})
