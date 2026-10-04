import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { SiteIcon } from './SiteIcon'
import type { Transport } from './transport'

/**
 * Covers {@link SiteIcon}.
 *
 * Scenarios:
 * - the icon of the site's connector is drawn at the size asked for
 * - encrypted transports get no badge; FTP that may skip TLS gets an amber one and an unencrypted connection a red one
 */
describe('SiteIcon', () => {
  it.each<[Transport, string | null]>([
    ['sftp', null],
    ['ftps', null],
    ['optional', 'bg-warn'],
    ['plain', 'bg-destructive'],
  ])('draws %s with its badge', (transport, badge) => {
    const { container } = render(<SiteIcon icon="gdrive" transport={transport} className="size-5" />)
    const wrapper = container.firstElementChild as HTMLElement

    expect(wrapper).toHaveAttribute('data-transport', transport)
    expect(wrapper).toHaveClass('size-5')
    expect(wrapper.querySelector('svg')).toHaveAttribute('data-icon', 'gdrive')
    if (badge === null) {
      expect(wrapper.querySelector('[data-badge]')).toBeNull()
    } else {
      expect(wrapper.querySelector('[data-badge]')).toHaveClass(badge)
    }
  })
})
