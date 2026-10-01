import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Illustration, type Scene } from './Illustration'

/**
 * Covers {@link Illustration}.
 *
 * Scenarios:
 * - every scene is drawn, decorative, with the outline ink and both brand colors
 * - the drawing takes the classes it is given
 */
describe('Illustration', () => {
  it.each<Scene>(['start', 'folder', 'transfers', 'sites'])('draws the %s scene in the brand colors', (scene) => {
    const { container } = render(<Illustration scene={scene} className="h-12" />)
    const svg = container.querySelector('svg') as SVGElement
    const inks = new Set([...svg.children].map((shape) => shape.getAttribute('class')))

    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveAttribute('data-scene', scene)
    expect(svg).toHaveClass('h-12')
    expect(inks).toEqual(new Set(['text-faint', 'text-(--brand-arrow)', 'text-(--brand-ribbon)']))
  })
})
