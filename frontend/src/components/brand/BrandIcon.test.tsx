import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { BrandIcon, type BrandIconName } from './BrandIcon'

/**
 * Covers {@link BrandIcon}.
 *
 * Scenarios:
 * - every icon is decorative, takes its classes, and draws its outline in the text color and its accent in the brand color
 */
describe('BrandIcon', () => {
  it.each<BrandIconName>(['transfer', 'logs', 'sites'])('draws %s in two tones', (name) => {
    const { container } = render(<BrandIcon name={name} className="size-3" />)
    const svg = container.querySelector('svg') as SVGElement
    const [base, accent] = svg.querySelectorAll('path')

    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveAttribute('data-icon', name)
    expect(svg).toHaveClass('size-3')
    expect(base).not.toHaveAttribute('class')
    expect(accent).toHaveClass('text-primary')
  })
})
